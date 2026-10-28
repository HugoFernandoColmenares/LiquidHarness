# 02 — Investigación: LFM2.5, formato de herramientas y runtime de inferencia

> **Estado:** Baseline de investigación · **Última verificación:** 2026-10-28
> **Fuentes primarias:** model cards de Hugging Face, documentación de Liquid AI y blogs oficiales.
> **Objetivo:** fijar qué puede y qué no puede hacer LFM2.5, cómo se le habla y cómo se ejecuta
> localmente desde Go sin sacrificar fiabilidad.

---

## 1. La familia LFM2.5

Liquid AI posiciona LFM2.5 como una familia de modelos **híbridos** (convoluciones + atención) para
**ejecución on-device** y **cargas agénticas**. La familia cubre dos órdenes de magnitud:

| Modelo | Parámetros | Contexto | Perfil | Uso recomendado |
| :--- | ---: | ---: | :--- | :--- |
| **LFM2.5-230M** | 230 M | 32 K–131 K* | Ultra-edge | Clasificación, extracción, móvil |
| **LFM2.5-1.2B-Instruct** | 1.2 B | 32 K–131 K* | Edge | Asistente ligero, RAG |
| **LFM2.5-1.2B-Thinking** | 1.2 B | 32 K–131 K* | Edge razonador | Tareas con razonamiento explícito |
| **LFM2.5-2.6B** | 2.69 B | **131 072** | Balance | **Modelo de referencia v1** |
| **LFM2.5-8B-A1B** | 8 B (1 B activos) | 131 072 | MoE edge | Mayor calidad, portátil |
| **LFM2.5-24B-A2B** | 24 B (2 B activos) | 131 072 | MoE workstation | Máxima calidad local |
| **LFM2.5-VL-1.6B** | 1.6 B | 32 K+ | Visión | Documentos con imagen |
| **LFM2.5-Audio-1.5B** | 1.5 B | — | Audio | Voz |

\* Verificar la ficha exacta de cada checkpoint; la familia comparte arquitectura pero no todos los
tamaños tienen el mismo contexto efectivo.

### 1.1 LFM2.5-2.6B en detalle (modelo de referencia)

| Propiedad | Valor |
| :--- | :--- |
| Parámetros totales | 2.69 B |
| Capas | 30 (22 bloques de convolución corta *double-gated* + 8 GQA) |
| Contexto | 131 072 tokens |
| Vocabulario | 128 000 |
| Entrenamiento | ~34 T tokens |
| Idiomas | 16 (EN, ES, FR, DE, IT, PT, AR, ZH, JA, KO, VI, TH, ID, HI, RU, PL) |
| Memoria de inferencia | < 2.5 GB (BF16; menos con GGUF Q4) |
| Throughput CPU | ~220 tok/s en Apple M5 Max; ~113 tok/s en Ryzen AI Max+ 395 |
| Sampling recomendado | `temperature=0.1`, `top_k=50`, `repetition_penalty=1.1` |
| Formato | Nativo (Transformers/vLLM/SGLang), GGUF, ONNX, MLX |
| Drafter especulativo | LFM2.5-2.6B-DSpark (328 M) → ~2.6× decoding |
| Licencia | LFM Open License v1.0 (revisar términos comerciales) |

### 1.2 Lo que el model card dice explícitamente

> "We recommend using it for agentic workloads, tool use, data extraction, RAG, and long-context
> workflows. **It is not recommended for agentic coding and knowledge-heavy tasks.**"

Esta frase es una restricción de producto, no un detalle. Implica:

- **Sí**: gestión de archivos, búsqueda documental, resúmenes, extracción estructurada, RAG,
  asistentes locales, automatización de carpetas, memoria personal.
- **No**: refactors de código, debugging complejo, tareas que requieren conocimiento enciclopédico,
  agentes de software autónomos.

El `project.md` actual habla de "file-system agent" y de un harness para LFM-2.5: es coherente si el
producto se posiciona como **agente local de conocimiento y archivos**, no como coding agent.

### 1.3 Benchmarks relevantes (2.6B)

| Benchmark | LFM2.5-2.6B | Qué mide |
| :--- | ---: | :--- |
| BFCLv4 | 56.88 | Function calling multi-turno |
| ToolSandbox | 77.83 | Uso de tools con estado |
| IFStruct | 85.49 | Instrucciones estructuradas |
| Multi-IF | 80.07 | Instrucciones multilingües |
| IFBench | 59.17 | Seguimiento de instrucciones |
| τ³-Bench Banking | 5.67 | Agente de dominio (difícil) |
| PinchBench | 68.22 | Tareas de asistente |
| Claw-Eval (EN) | 62.85 | Agente general |

Interpretación: **excelente para su tamaño en tool calling e instrucciones**, pero con techo claro en
razonamiento de dominio y planificación larga. El harness debe compensar con estructura, no con
prompts más largos.

---

## 2. Plantilla de chat

LFM2.5 usa un formato **ChatML-like**:

```text
<|startoftext|><|im_start|>system
You are a helpful assistant trained by Liquid AI.<|im_end|>
<|im_start|>user
What is C. elegans?<|im_end|>
<|im_start|>assistant
```

Puntos críticos:

- El token `<|startoftext|>` abre la secuencia.
- Los roles van entre `<|im_start|>` y `<|im_end|>`.
- El modelo es **reasoning-first**: la plantilla añade ` thinking` al inicio de la respuesta del
  asistente. La respuesta puede contener `...reasoning...</think>answer`.
- El rol `tool` transporta el resultado de la herramienta.
- El vocabulario incluye tokens especiales de tool calling.

### 2.1 Regla de oro

**No reimplementar la plantilla a mano en Go.** Es frágil y rompe con cada checkpoint. Opciones, en
orden de preferencia:

1. Delegar en el backend (`llama-server` con `--jinja`, vLLM, SGLang) que aplica la plantilla del
   GGUF/modelo.
2. Usar el `chat_template.jinja` del checkpoint con un renderizador probado (solo si el backend no lo
   hace).
3. Tener una implementación Go **con golden tests** contra la plantilla oficial, y fallar cerrado si
   no coincide.

El harness puede mantener un modelo canónico de mensajes, pero la **serialización final** debe ser
del backend siempre que sea posible.

---

## 3. Tool calling en LFM2.5

### 3.1 Flujo de cuatro pasos

1. **Definir** las tools como JSON en el system prompt (o vía `apply_chat_template(tools=...)`).
2. **Generar** una respuesta con las llamadas entre tokens especiales.
3. **Ejecutar** y devolver el resultado con rol `tool`.
4. **Regenerar** para que el modelo interprete el resultado y responda.

### 3.2 Formato por defecto (Pythonic)

```text
<|startoftext|><|im_start|>system
List of tools: [{"name": "get_candidate_status", "description": "...", "parameters": {...}}]<|im_end|>
<|im_start|>user
What is the current status of candidate ID 12345?<|im_end|>
<|im_start|>assistant
<|tool_call_start|>[get_candidate_status(candidate_id="12345")]<|tool_call_end|>Checking the current status...<|im_end|>
<|im_start|>tool
[{"candidate_id": "12345", "status": "Interview Scheduled"}]<|im_end|>
<|im_start|>assistant
The candidate ...<|im_end|>
```

Características:

- Llamada = lista Python entre `<|tool_call_start|>` y `<|tool_call_end|>`.
- Argumentos con sintaxis Python (`key="value"`, números, listas, dicts).
- Puede haber **varias llamadas** en la misma lista.
- El modelo puede escribir texto antes/después de la llamada.

### 3.3 Formato JSON (recomendado para el harness)

Añadiendo al system prompt una instrucción explícita:

```text
Output function calls as JSON.
```

El modelo emite:

```text
<|tool_call_start|>[{"name":"fs_read","arguments":{"path":"docs/notes.md"}}]<|tool_call_end|>
```

Ventajas para Go:

- `encoding/json/v2` parsea directo.
- Errores de sintaxis son detectables y corregibles.
- No hay que escribir un parser de Python.

Riesgo: el modelo puede ignorar la instrucción y emitir Pythonic. El parser debe aceptar **ambos** y
normalizar, con tests de regresión.

### 3.4 Estrategia de parsing robusta

```text
raw
 ├─ ¿contiene <|tool_call_start|>?
 │   ├─ sí → extraer bloque → intentar JSON → intentar Pythonic → error
 │   └─ no → ¿hay bloque ```json con "name"/"arguments"? → intentar
 ├─ validar contra JSON Schema
 ├─ si falla → devolver tool_error con el esquema y el error exacto
 └─ si hay múltiples → ejecutar en orden (o en paralelo si son read-only)
```

Reglas:

- Nunca ejecutar una tool si el bloque no se ha parseado y validado.
- Si el parseo falla, el mensaje de error debe incluir el bloque recibido (truncado) y un ejemplo
  correcto.
- Limitar el número de llamadas por respuesta (p. ej. 8) para evitar explosiones.
- Soportar *streaming*: no ejecutar hasta tener el bloque completo; mostrar "preparando tool…" en UI.

### 3.5 Formato de resultado

LFM2.5 espera el resultado como contenido del rol `tool`. Para máxima compatibilidad:

```json
{
  "ok": true,
  "data": { "...": "..." },
  "meta": { "truncated": false, "bytes": 1234 }
}
```

No incluir markdown ni explicaciones dentro del JSON; el modelo ya recibe el contexto. Si el
resultado es texto plano (p. ej. contenido de archivo), envolverlo igualmente para que el modelo
distinga datos de instrucciones.

---

## 4. Cuantización y huella de memoria

| Formato | Calidad relativa | RAM/VRAM aprox. (2.6B) | Uso |
| :--- | :--- | ---: | :--- |
| BF16 / F16 | 100 % | ~5.4 GB | GPU con margen |
| Q8_0 | ~99 % | ~2.9 GB | CPU/GPU equilibrado |
| Q6_K | ~98 % | ~2.3 GB | Buena relación calidad/tamaño |
| **Q5_K_M** | ~97 % | ~2.0 GB | **Default recomendado** |
| Q4_K_M | ~95 % | ~1.7 GB | Hardware modesto |
| Q4_0 | ~92 % | ~1.6 GB | Mínimo |
| Q3_K_M | ~88 % | ~1.4 GB | Solo si la RAM es crítica |

> Los tamaños son estimaciones; el archivo GGUF real y el KV cache dependen del contexto. Con 131K
> tokens el KV cache puede superar el peso del modelo: presupuestar contexto explícitamente.

### 4.1 KV cache

- El coste crece con `context_length × capas × dimensiones`.
- Para 2.6B, usar 8K–32K de contexto en uso real suele ser suficiente y mucho más rápido.
- `llama-server` permite `--ctx-size`, `--cache-type-k`, `--cache-type-v` (cuantizar el KV cache).
- El harness debe exponer `model.context_window` y `model.max_output_tokens` como configuración, no
  asumir 131K.

### 4.2 Speculative decoding

LFM2.5-2.6B-DSpark (328 M) acelera ~2.6× con **salidas idénticas** en SGLang y Apple Silicon (Metal).
En llama.cpp, el soporte depende de la build; no es requisito de v1. El instalador puede ofrecerlo
como perfil "fast" cuando esté disponible.

---

## 5. Backends de inferencia

| Backend | Ventajas | Limitaciones | Integración Go |
| :--- | :--- | :--- | :--- |
| **llama.cpp (`llama-server`)** | GGUF, CPU/GPU, un binario, OpenAI-compatible, `--jinja` | Gestión manual del proceso; tool parsing variable | HTTP local (recomendado) o CGO |
| **vLLM** | Alto throughput GPU, OpenAI API, tool parser `lfm2` | Python, GPU, pesado | HTTP |
| **SGLang** | Throughput, speculative, tool parser `lfm2` | Python, GPU | HTTP |
| **MLX** | Óptimo en Apple Silicon | Solo macOS/Apple | HTTP (servidor MLX) |
| **ONNX Runtime** | Cross-platform, aceleración | Menos ergonómico en Go | CGO o sidecar |
| **LM Studio** | Fácil para usuarios | App externa, no "zero dependency" | HTTP |
| **Ollama** | Fácil, ecosistema | Envoltorio de llama.cpp, menos control | HTTP |

### 5.1 Decisión para v1

**Runtime por defecto: HTTP OpenAI-compatible** contra un `llama-server` local gestionado por el
harness (o uno ya existente). Ventajas:

- `CGO_ENABLED=0` en el binario principal → cross-compile trivial, instalador simple.
- Aislamiento de fallos: si el modelo cae, el harness sigue vivo.
- Permite GPU, CPU, remoto y otros backends sin recompilar.
- Facilita tests con un servidor falso.

**Runtime embebido (opt-in):** build tag `embedded` con CGO y llama.cpp. Se distribuye como artefacto
separado (`liquidharness-embedded`) para usuarios que quieren un solo proceso. No es el default.

**Runtime pure Go (experimental):** `goccy/go-llama` demuestra que es posible, pero la madurez, el
rendimiento y la paridad de plantillas aún no justifican ponerlo en el camino crítico. Se evalúa en
un spike, no en v1.

### 5.2 Contrato con el backend

El harness asume este subconjunto de la API OpenAI:

| Endpoint | Uso |
| :--- | :--- |
| `GET /health` | Liveness |
| `GET /v1/models` | Descubrir modelo y contexto |
| `POST /v1/chat/completions` | Chat con `tools`, `stream`, `temperature`, `top_k`, `max_tokens` |
| `POST /tokenize` (llama.cpp) | Conteo exacto de tokens (opcional) |
| `GET /props` (llama.cpp) | Contexto, plantilla, parámetros (opcional) |

Si el backend no expone `/tokenize`, el harness usa una estimación conservadora y **marca** los
conteos como aproximados.

### 5.3 Arranque y supervisión del proceso

```text
1. ¿Hay un endpoint configurado y sano? → usarlo.
2. ¿No? → buscar `llama-server` en PATH / carpeta gestionada.
3. ¿No? → ofrecer descarga del runtime + modelo (instalador).
4. Arrancar con flags seguros: --host 127.0.0.1 --port 0 (puerto efímero) --jinja
   --ctx-size N --model <gguf> --no-webui.
5. Esperar /health con timeout y backoff.
6. Si el proceso muere → reiniciar con límite; si falla 3 veces → modo degradado.
7. Al salir → SIGTERM, esperar, SIGKILL tras timeout.
```

Nunca escuchar en `0.0.0.0` por defecto. Nunca exponer el servidor de modelo a la red local sin auth.

---

## 6. Integración con Go

### 6.1 Puerto de modelo

```go
// internal/ports/model.go
package ports

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role
	Content    string
	Reasoning  string
	ToolCalls  []ToolCall
	ToolCallID string
	Name       string
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments []byte // JSON validado
}

type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolSchema
	Temperature float64
	TopK        int
	MaxTokens   int
	Stream      bool
}

type ChatResponse struct {
	Message      Message
	FinishReason string
	Usage        Usage
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type Model interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (Stream, error)
	Info(ctx context.Context) (ModelInfo, error)
}

type Stream interface {
	Recv() (Delta, error) // io.EOF al terminar
	Close() error
}

type Delta struct {
	Content   string
	Reasoning string
	ToolCall  *ToolCall
	Done      bool
}
```

### 6.2 Cliente HTTP mínimo

Para el camino crítico no hace falta un SDK:

```go
type OpenAIClient struct {
	baseURL string
	http    *http.Client
	apiKey  string
}

func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body, err := json.Marshal(toWireRequest(req))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("encode chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("model request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, parseAPIError(resp)
	}
	var wire wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return ChatResponse{}, fmt.Errorf("decode response: %w", err)
	}
	return fromWireResponse(wire), nil
}
```

Reglas de implementación:

- `http.Client` con `Timeout` global y `Transport` con timeouts finos.
- Reutilizar conexiones (`MaxIdleConnsPerHost`).
- Leer el body con límite (`io.LimitReader`) para no aceptar respuestas gigantes.
- Clasificar errores: `ErrTransient` (5xx, timeout, connection reset) vs `ErrPermanent` (4xx de
  validación).
- Reintentar solo transient, con backoff exponencial + jitter y respetando `Retry-After`.
- No reintentar si el request incluía tool calls ya ejecutadas (peligro de duplicar efectos).

### 6.3 SSE y streaming

El streaming es obligatorio para UX (primer token < 1.5 s). Puntos finos:

- Parsear `data: {...}` línea a línea; ignorar comentarios `:`.
- Soportar `data: [DONE]`.
- Acumular `tool_calls` por índice: el nombre llega en el primer chunk, los argumentos en fragmentos.
- Cancelar el `http.Request` con `context` cuando el usuario pulsa `Esc`.
- Cerrar el body siempre, incluso en error.
- No renderizar tokens de razonamiento como respuesta final; van a un bloque colapsable.

### 6.4 Conteo de tokens

Tres niveles:

1. **Exacto**: endpoint `/tokenize` del backend para el prompt; `usage` para la respuesta.
2. **Aproximado**: `ceil(len(runes)/3.5)` para inglés, `/2.5` para español/código; siempre marcado
   como estimación.
3. **Presupuesto conservador**: usar el máximo de (exacto, aproximado) para no pasarse.

El tokenizer de LFM2.5 tiene 128K entradas; no embeber una tabla enorme en el binario. Si se necesita
offline total, incluir un tokenizer JSON opcional como asset y cargarlo con una librería, pero el
default es preguntar al backend.

---

## 7. Fiabilidad del modelo pequeño: patrones que funcionan

### 7.1 System prompt

Estructura recomendada (≤ 1 200 tokens):

```text
Rol: agente local de archivos y conocimiento.
Objetivo del turno: {objetivo}
Reglas:
1. Usa solo las tools listadas.
2. Una tool por paso; espera el resultado antes de la siguiente.
3. No inventes rutas; usa fs_list para descubrir.
4. Si una tool falla, lee el error y corrige.
5. No pidas confirmación al usuario; el harness la gestiona.
6. Responde en el idioma del usuario.
Formato de tool call: JSON.
```

Evitar:

- listas de 40 reglas;
- ejemplos largos de cada tool;
- instrucciones contradictorias;
- pedir "piensa paso a paso" cuando el modelo ya tiene ` thinking`.

### 7.2 Descripciones de tools

- Verbo + objeto + límite: "Read a UTF-8 text file inside the workspace. Returns at most 2000 lines."
- Indicar cuándo **no** usarla.
- Documentar el formato de error.
- Esquemas planos: máx. 2 niveles de anidación.
- Enums en lugar de strings libres cuando sea posible.

### 7.3 Self-correction

El harness debe devolver errores **accionables** y permitir un reintento. Ejemplo:

```json
{
  "ok": false,
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "field 'limit' must be between 1 and 2000",
    "hint": "Use limit=200 or omit it to use the default.",
    "retryable": true
  }
}
```

Con 2.6B, un error claro + un reintento suele resolver; tres fallos del mismo tipo deben cortar el
turno y escalar al usuario.

### 7.4 Verificación

Para tareas de escritura:

1. `fs_edit` con hash de precondición.
2. `fs_read` del rango editado.
3. Comprobar que el contenido esperado está presente.
4. Si no, corregir o reportar.

El harness puede hacer la verificación automáticamente (post-condition) y devolver el resultado al
modelo, sin confiar en que el modelo la pida.

---

## 8. Contexto largo: qué esperar realmente

131K es una ventana, no una invitación. Con 2.6B:

| Contexto usado | Efecto observado típico | Recomendación |
| :--- | :--- | :--- |
| < 8K | Rápido y preciso | Zona ideal |
| 8K–32K | Buen equilibrio | Default de trabajo |
| 32K–64K | Latencia sube; atención se diluye | Solo si la tarea lo exige |
| > 64K | Mayor riesgo de ignorar instrucciones | Compactar o dividir |

Estrategia del harness:

- Presupuesto por defecto 32K, configurable.
- RAG para traer solo fragmentos relevantes.
- Compactación proactiva al 80 %.
- Resumen de sesión persistido para no re-enviar todo.
- "Contexto estable" al inicio para reutilizar KV cache.

---

## 9. Licencia y distribución del modelo

- El harness puede ser Apache-2.0, pero **los pesos no**: LFM2.5 usa la **LFM Open License v1.0**.
- El instalador no debe empaquetar pesos en el binario; debe descargarlos con:
  - URL y versión fijadas;
  - SHA-256 publicado;
  - licencia mostrada y aceptada;
  - posibilidad de usar un mirror corporativo.
- Revisar términos comerciales antes de distribuir un producto de pago.
- Mantener un archivo `THIRD_PARTY_NOTICES.md` con la licencia del modelo, llama.cpp y dependencias.

---

## 10. Riesgos específicos del runtime

| Riesgo | Impacto | Mitigación |
| :--- | :--- | :--- |
| Plantilla de chat desincronizada | Tool calls rotas | Delegar en backend + golden tests |
| Tool parser del backend distinto | Llamadas no detectadas | Parser propio tolerante + tests |
| Contexto efímero mal calculado | Overflow en producción | `/tokenize` + presupuesto conservador |
| Modelo alucina tools | Errores de validación | Lista cerrada + error con tools válidas |
| Latencia en CPU | UX pobre | Streaming, perfiles, Q4/Q5, contexto moderado |
| RAM insuficiente | Crash/OOM | Preflight de memoria + perfil recomendado |
| Proceso huérfano | Puerto ocupado | Gestión de ciclo de vida + PID file |
| Actualización de pesos | Cambio de comportamiento | Versionado + evals de regresión |
| Licencia | Bloqueo comercial | Revisión legal + avisos claros |

---

## 11. Decisiones derivadas

| ID | Decisión | Motivo |
| :--- | :--- | :--- |
| L-01 | Modelo de referencia v1: LFM2.5-2.6B (Q5_K_M) | Mejor equilibrio tamaño/calidad |
| L-02 | Runtime default: `llama-server` vía HTTP OpenAI-compatible | CGO=0, aislamiento, flexibilidad |
| L-03 | Tool calls en JSON, parser tolerante a Pythonic | Fiabilidad y compatibilidad |
| L-04 | Presupuesto de contexto default 32K | Latencia y precisión |
| L-05 | Streaming obligatorio en TUI | Percepción de velocidad |
| L-06 | No embeber pesos; descarga verificada | Licencia y tamaño |
| L-07 | Perfiles `cpu`/`balanced`/`quality`/`remote` | Hardware heterogéneo |
| L-08 | Evals con modelo real en nightly, mock en PR | Coste/velocidad |

---

## 12. Referencias

1. Liquid AI — LFM2.5-2.6B (model card):
   <https://huggingface.co/LiquidAI/LFM2.5-2.6B>
2. Liquid AI — Docs, LFM2.5-2.6B:
   <https://docs.liquid.ai/lfm/models/lfm25-2.6b>
3. Liquid AI — Tool Use:
   <https://docs.liquid.ai/lfm/key-concepts/tool-use>
4. Liquid AI — Chat Template:
   <https://docs.liquid.ai/lfm/key-concepts/chat-template>
5. Liquid AI — *LFM2.5-2.6B: Deploy Agents Everywhere*:
   <https://www.liquid.ai/blog/lfm2-5-2-6b>
6. Liquid AI — *LFM2.5-24B-A2B: Built for Fast On-Device Agents*:
   <https://www.liquid.ai/blog/lfm2-5-24b-a2b>
7. Liquid AI — *LFM2.5-8B-A1B*:
   <https://www.liquid.ai/blog/lfm2-5-8b-a1b>
8. Liquid AI — Agent Harnesses guide:
   <https://docs.liquid.ai/examples/agent-harnesses>
9. Liquid AI — llama.cpp inference:
   <https://docs.liquid.ai/lfm/inference/llama-cpp>
10. llama.cpp — servidor y API:
    <https://github.com/ggml-org/llama.cpp/tree/master/tools/server>
11. goccy/go-llama — llama.cpp en Go puro (experimental):
    <https://github.com/goccy/go-llama>
12. LFM2 Technical Report (arXiv 2511.23404):
    <https://arxiv.org/abs/2511.23404>
