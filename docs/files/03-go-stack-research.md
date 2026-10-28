# 03 — Investigación: stack Go 1.27 para un harness local

> **Estado:** Baseline de investigación · **Última verificación:** 2026-10-28
> **Principio rector:** *standard library first*. Cada dependencia debe justificar su coste en
> binario, superficie de ataque, mantenimiento y licencia.

---

## 1. Versión de Go y lenguaje

**Go 1.27** (19 de agosto de 2026) es la base recomendada. El `project.md` dice 1.22+, pero fijar
1.27 aporta mejoras directamente relevantes:

| Característica | Por qué importa aquí |
| :--- | :--- |
| `encoding/json/v2` + `jsontext` | Parsing más rápido y estricto de tool calls y config |
| `uuid` en stdlib | IDs de sesión/tool call sin dependencia |
| Métodos genéricos | Utilidades de repositorio y paginación sin duplicar |
| `testing/synctest` | Tests deterministas de timeouts y concurrencia |
| Perfil `goroutineleak` | Detección de fugas en el loop y el runtime |
| `new(expr)` | Constructores más limpios |
| `go fix` modernizadores | Migraciones automáticas seguras |

### 1.1 Directiva de módulo

```go
module github.com/HugoFernandoColmenares/LiquidHarness

go 1.27.0

toolchain go1.27.2
```

- Un solo módulo. No hace falta multi-módulo en v1.
- `toolchain` fija la versión de build; CI usa la misma.
- Sin `replace` en `main`.
- `go.sum` versionado; `GOFLAGS=-mod=readonly` en CI.

### 1.2 Estilo

- `gofmt` obligatorio; `gofumpt` en CI.
- Nombres cortos, paquetes pequeños, interfaces en el consumidor.
- `context.Context` como primer parámetro en todo lo que hace I/O.
- Errores envueltos con `%w` y mensajes en minúscula.
- Sin `init()` salvo para registrar drivers de forma justificada.
- Sin variables globales mutables; el estado vive en structs inyectados.
- `slog` para logs; nada de `fmt.Println` fuera de la UI.
- Comentarios en inglés en código público; docs internas pueden ser en español.

---

## 2. Matriz de stack

| Capa | Elección | Alternativas | Decisión |
| :--- | :--- | :--- | :--- |
| CLI | `spf13/cobra` + `pflag` | `urfave/cli`, `flag` | ✅ Cobra: subcomandos, help, completions |
| Config | TOML (`BurntSushi/toml`) + env + flags | Viper, Koanf | ✅ Mínimo y explícito |
| Logging | `log/slog` (stdlib) | zap, zerolog | ✅ Stdlib |
| TUI | `charm.land/bubbletea/v2` | tview, tcell | ✅ Elm architecture, ecosistema |
| Estilos | `charm.land/lipgloss/v2` | — | ✅ Tokens y layout |
| Widgets | `charm.land/bubbles/v2` | — | ✅ list, viewport, textinput, table |
| Markdown | `charm.land/glamour/v2` | — | ✅ Render en terminal |
| Formularios | `charm.land/huh/v2` | — | ✅ Wizard first-run |
| LLM client | `net/http` propio | `openai-go`, langchaingo | ✅ Sin SDK en el core |
| MCP | `github.com/modelcontextprotocol/go-sdk` | `mark3labs/mcp-go` | ✅ Oficial, Tier 1 |
| SQLite | `modernc.org/sqlite` | `mattn/go-sqlite3` | ✅ CGO=0 |
| Vector search | BLOB + coseno en Go; `sqlite-vec` opcional | chromem-go, LanceDB | ✅ Portable y suficiente |
| Full-text | SQLite FTS5 | Bleve | ✅ Ya incluido |
| JSON Schema | `github.com/google/jsonschema-go/jsonschema` | santhosh-tekuri/jsonschema | ✅ Integrado con MCP SDK |
| YAML | `gopkg.in/yaml.v3` | — | ✅ Solo para evals/config avanzada |
| Diff | `github.com/sergi/go-diff` (display) | gotextdiff | ✅ Simple |
| Fuzzy | `github.com/sahilm/fuzzy` | — | ✅ Command palette |
| Concurrencia | `golang.org/x/sync/errgroup`, `semaphore` | — | ✅ Bounded parallelism |
| Testing | `testing` + `go-cmp` | testify | ✅ Menos magia |
| Lint | `golangci-lint` v2 + `gofumpt` + `govulncheck` | — | ✅ |
| Release | GoReleaser v2 + cosign + syft | — | ✅ |

> Las rutas `charm.land/*/v2` son las oficiales de Charm v2 (vanity import). Verificar la versión
> exacta de `glamour` y `huh` al iniciar la implementación; si aún no tienen v2 estable, usar la
> última v1 compatible y aislarla tras una interfaz interna.

### 2.1 Dependencias totales objetivo

| Categoría | Nº aprox. | Nota |
| :--- | ---: | :--- |
| CLI | 2 | cobra, pflag |
| TUI | 5–6 | bubbletea, lipgloss, bubbles, glamour, huh |
| Storage | 1 | modernc sqlite (más sus transitivas) |
| MCP | 1 | go-sdk |
| Utilidades | 3–4 | toml, yaml, go-cmp (test), fuzzy, diff |
| **Total directas** | **≤ 15** | Revisar en cada release |

Regla: una dependencia nueva requiere justificación en el PR, revisión de licencia y `govulncheck`.

---

## 3. CLI

### 3.1 Superficie de comandos

```text
liquidharness
├── init                # wizard de primera ejecución
├── run [prompt]        # sesión interactiva (TUI) o una tarea
├── chat                # alias de run --interactive
├── ask <prompt>        # one-shot, headless
├── serve               # HTTP local / MCP server
├── models              # list | pull | verify | remove | use
├── config              # get | set | edit | path | validate
├── session             # list | show | resume | delete | export
├── memory              # list | add | search | forget | export
├── rag                 # index | search | stats | reindex
├── eval                # run | list | report | compare
├── doctor              # diagnóstico del entorno
├── update              # actualización firmada
├── completion          # bash | zsh | fish | powershell
└── version
```

### 3.2 Convenciones

- `--json` en todos los comandos de inspección (salida estable, versionada).
- `--yes` para automatización (solo R1); R2+ requiere `--allow-risk=Rn`; nunca implícito.
- `--profile` para presets.
- `--workspace` para el jail.
- Códigos de salida documentados (ver arquitectura §15).
- `--help` con ejemplos reales, no solo flags.
- Sin colores si `NO_COLOR` o `!isatty`.
- Los comandos destructivos exigen `--force` o confirmación.

### 3.3 Cobra: estructura

```go
func newRootCmd(app *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:           "liquidharness",
		Short:         "Local-first agent harness for LFM2.5",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return app.Configure(cmd.Context(), cmd.Flags())
		},
	}
	root.PersistentFlags().String("config", "", "config file (default: OS config dir)")
	root.PersistentFlags().String("profile", "", "configuration profile")
	root.PersistentFlags().Bool("json", false, "machine-readable output")
	root.AddCommand(
		newRunCmd(app), newAskCmd(app), newServeCmd(app),
		newModelsCmd(app), newConfigCmd(app), newSessionCmd(app),
		newMemoryCmd(app), newRAGCmd(app), newEvalCmd(app),
		newDoctorCmd(app), newUpdateCmd(app), newVersionCmd(app),
	)
	return root
}
```

`main.go` solo hace:

```go
func main() {
	if err := cli.Execute(context.Background(), os.Args[1:]); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
```

---

## 4. Configuración

### 4.1 Precedencia

```text
flags  >  env  >  .env (solo desarrollo)  >  project config  >  user config  >  defaults
```

- **Flags**: overrides de una ejecución.
- **Env**: `LIQUIDHARNESS_*`; útil en CI y contenedores.
- **`.env`**: solo se carga si `--env-file` o `LIQUIDHARNESS_ENV_FILE`; nunca se commitea.
- **Project config**: `.liquidharness/config.toml` en el workspace.
- **User config**: `os.UserConfigDir()/liquidharness/config.toml`.
- **Defaults**: compilados y documentados.

### 4.2 Formato TOML

```toml
# ~/.config/liquidharness/config.toml
[general]
profile = "balanced"
workspace = "/home/dev/project"
language = "es"

[model]
provider = "openai-compatible"
base_url = "http://127.0.0.1:8080/v1"
name = "LFM2.5-2.6B"
context_window = 32768
max_output_tokens = 2048
temperature = 0.1
top_k = 50
repetition_penalty = 1.1
timeout = "120s"

[agent]
max_steps = 24
max_tool_calls = 32
max_turn_duration = "10m"
parallel_readonly_tools = 4

[policy]
mode = "ask"            # read-only | ask | allow
allow_shell = false
allow_network = false
max_write_bytes = 1048576

[rag]
enabled = true
chunk_tokens = 512
chunk_overlap = 64
top_k = 6
embedding_model = "bge-m3-Q5_K_M.gguf"

[ui]
theme = "liquid-dark"
animations = true
mouse = false
```

### 4.3 Resolución en Go

```go
type Config struct {
	General GeneralConfig `toml:"general"`
	Model   ModelConfig   `toml:"model"`
	Agent   AgentConfig   `toml:"agent"`
	Policy  PolicyConfig  `toml:"policy"`
	RAG     RAGConfig     `toml:"rag"`
	UI      UIConfig      `toml:"ui"`
}

func Load(paths Paths, flags Flags, getenv func(string) string) (Config, error) {
	cfg := Defaults()
	if err := mergeFile(&cfg, paths.User); err != nil { return Config{}, err }
	if err := mergeFile(&cfg, paths.Project); err != nil { return Config{}, err }
	if err := mergeEnv(&cfg, getenv); err != nil { return Config{}, err }
	if err := mergeFlags(&cfg, flags); err != nil { return Config{}, err }
	return cfg, cfg.Validate()
}
```

Reglas:

- `Validate()` devuelve errores con el campo exacto.
- Sin `map[string]any` en runtime: structs tipados.
- Secretos **no** van en TOML; van en keyring o `.env` con permisos 0600.
- `config path` y `config validate` son obligatorios.
- Migración de config versionada (`config_version`).

---

## 5. TUI

### 5.1 Por qué Bubble Tea v2

- Modelo Elm: `Init/Update/View`, estado explícito, testeable.
- v2 trae renderer optimizado, color downsampling, teclado progresivo, clipboard, modo 2026/2027,
  `tea.View` declarativo.
- Ecosistema: Lip Gloss (estilos), Bubbles (widgets), Glamour (markdown), Huh (forms).
- Alternativas: `tview` (imperativo, más pesado), `tcell` (bajo nivel).

### 5.2 Arquitectura TUI

```text
internal/tui/
├── app.go            # tea.Model raíz, router de pantallas
├── keys.go           # keymap centralizado
├── theme/
│   ├── tokens.go     # design tokens (fuente de verdad)
│   ├── theme.go      # Theme, dark/light, NO_COLOR
│   └── styles.go     # estilos Lip Gloss derivados
├── components/
│   ├── header.go
│   ├── statusbar.go
│   ├── transcript.go
│   ├── message.go
│   ├── reasoning.go
│   ├── toolcard.go
│   ├── diff.go
│   ├── approval.go
│   ├── composer.go
│   ├── palette.go
│   ├── toast.go
│   └── modal.go
├── screens/
│   ├── chat.go
│   ├── sessions.go
│   ├── memory.go
│   ├── rag.go
│   ├── eval.go
│   └── wizard.go
└── testdata/         # golden views 80x24, 120x40
```

Regla: la TUI **no** llama al modelo ni a las tools directamente. Envía comandos a la capa de
aplicación y recibe eventos. Esto permite headless y tests.

### 5.3 Eventos

```go
type Event interface{ isEvent() }

type TokenDelta struct{ Text, Reasoning string }
type ToolStarted struct{ Call ToolCallView }
type ToolFinished struct{ Call ToolCallView; Result ToolResultView }
type ApprovalRequested struct{ Request ApprovalView }
type TurnFinished struct{ Reason string }
type ErrorRaised struct{ Err error }
```

La TUI se suscribe a un canal/`iter.Seq[Event]`; el agente no conoce Bubble Tea.

---

## 6. Persistencia

### 6.1 SQLite puro Go

`modernc.org/sqlite` evita CGO y permite cross-compile. Configuración obligatoria:

```go
dsn := "file:" + path +
	"?_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=synchronous(NORMAL)"
db, err := sql.Open("sqlite", dsn)
db.SetMaxOpenConns(1) // escritor único; lecturas con conexión separada si hace falta
```

- WAL para concurrencia lectura/escritura.
- Una sola conexión de escritura evita `SQLITE_BUSY`.
- Migraciones embebidas con `embed` y una tabla `schema_migrations`.
- `VACUUM`/`PRAGMA optimize` periódico.
- Backup con `VACUUM INTO`.

### 6.2 Vectores

**Decisión v1:** almacenar embeddings como `BLOB` (float32 little-endian) y calcular similitud coseno
en Go. Para ≤ 100 000 chunks de 768 dimensiones:

- memoria: ~300 MB si se cargan todos (no hacerlo);
- query: leer candidatos filtrados por metadata/FTS y calcular coseno en streaming;
- latencia objetivo: < 50 ms para 10 000 chunks.

`sqlite-vec` queda como **optimización opcional** (build tag `sqlitevec`), no como requisito. Así se
evita depender de una extensión nativa y se mantiene `CGO_ENABLED=0`.

### 6.3 FTS5

```sql
CREATE VIRTUAL TABLE chunks_fts USING fts5(
  content,
  path UNINDEXED,
  chunk_id UNINDEXED,
  tokenize = 'unicode61 remove_diacritics 2'
);
```

Búsqueda híbrida: `0.6 * vector_score + 0.4 * bm25_score`, normalizados. Configurable.

### 6.4 Alternativas descartadas

| Opción | Por qué no |
| :--- | :--- |
| bbolt | Sin SQL, sin FTS, consultas manuales |
| Badger | Más pesado, sin SQL |
| chromem-go | Todo en RAM; bueno para prototipo, no para persistencia |
| LanceDB | Dependencia nativa/FFI, sobredimensionado |
| Postgres | Rompe local-first y zero-ops |

---

## 7. MCP

### 7.1 Cliente

```go
client := mcp.NewClient(&mcp.Implementation{Name: "liquidharness", Version: version.Version}, nil)
transport := &mcp.CommandTransport{Command: exec.CommandContext(ctx, "npx", "-y", server)}
session, err := client.Connect(ctx, transport, nil)
if err != nil { return err }
defer session.Close()

tools, err := session.ListTools(ctx, nil)
```

Reglas:

- Namespace: `mcp__<server>__<tool>` para evitar colisiones.
- Timeout por llamada y por descubrimiento.
- Re-validar la lista cuando llegue `tools/list_changed`.
- Un servidor MCP es no confiable: riesgo mínimo R1; escritura/exec requieren aprobación.
- Limitar tamaño de resultados y número de tools.
- Registrar la versión del servidor y el hash de su configuración.

### 7.2 Servidor

`liquidharness serve --mcp` expone:

- `lh_run` (tarea one-shot con presupuesto),
- `lh_search` (RAG),
- `lh_memory` (lectura/escritura),
- `lh_session` (estado).

Con auth local (token efímero) y sin exponer tools destructivas por defecto.

---

## 8. Concurrencia

### 8.1 Reglas

1. El estado de sesión tiene **un solo escritor** (actor/loop). Nada de mutex repartidos.
2. Las tools read-only pueden ejecutarse en paralelo con `errgroup` + `semaphore` (límite 4).
3. Las tools de escritura se ejecutan **en serie** y en el orden emitido.
4. Todo `goroutine` debe tener `context` y una forma de terminar.
5. `panic` en una tool se recupera y se convierte en error de tool.
6. Sin `time.Sleep` en producción; usar timers y `context`.

### 8.2 Patrón de paralelismo

```go
func (d *Dispatcher) executeReadOnly(ctx context.Context, calls []Call) ([]Result, error) {
	results := make([]Result, len(calls))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(d.maxParallel)
	for i, call := range calls {
		i, call := i, call
		g.Go(func() error {
			res, err := d.executeOne(ctx, call)
			if err != nil {
				return err // o guardar error y continuar, según política
			}
			results[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}
```

- Resultados en orden original (slice indexado), no en orden de finalización.
- Un error de una tool no debe cancelar las demás si son independientes; usar `errors.Join`.
- El timeout global del turno cancela todo.

### 8.3 Tests de concurrencia

`testing/synctest` (Go 1.26+) permite probar timeouts y carreras de forma determinista. Combinar con
`-race` en CI.

---

## 9. Errores y resiliencia

### 9.1 Taxonomía

```go
var (
	ErrUser       = errors.New("user error")        // input inválido
	ErrPolicy     = errors.New("policy denied")     // permiso
	ErrTool       = errors.New("tool failed")       // ejecución
	ErrModel      = errors.New("model failed")      // proveedor
	ErrTransient  = errors.New("transient")         // reintentar
	ErrContext    = errors.New("context overflow")  // compactar
	ErrCancelled  = errors.New("cancelled")         // usuario/señal
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
)
```

- Cada error de dominio implementa `Code() string`, `Retryable() bool`, `UserMessage() string`.
- `errors.Is/As` para clasificar; nunca comparar strings.
- En el límite HTTP/CLI se mapea a código de salida y JSON.
- Los errores internos se loguean completos; al usuario/modelo se le da un mensaje accionable.

### 9.2 Reintentos

```go
func retry(ctx context.Context, policy Policy, fn func(context.Context) error) error {
	for attempt := 0; ; attempt++ {
		err := fn(ctx)
		if err == nil { return nil }
		if !policy.Retryable(err) || attempt >= policy.MaxAttempts {
			return err
		}
		delay := policy.Backoff(attempt) // exponential + jitter
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}
```

- Solo para operaciones idempotentes.
- Respetar `Retry-After`.
- Circuit breaker simple para el modelo: tras N fallos, modo degradado durante 30 s.

---

## 10. Rendimiento

| Área | Técnica |
| :--- | :--- |
| JSON | `encoding/json/v2`; reutilizar `bytes.Buffer` con `sync.Pool` |
| Streaming | `bufio.Scanner` con buffer ampliado; evitar `string` por token |
| SQLite | Prepared statements; batch inserts en transacción; índices por `session_id` |
| Vectores | `float32`; SIMD del compilador; evitar `[]float64` |
| RAG | Cache de embeddings por hash de contenido; no reindexar sin cambios |
| TUI | Render incremental; no reconstruir todo el transcript por token |
| Arranque | Lazy init de RAG/modelo; `doctor` no carga el modelo |
| Memoria | Liberar transcripts antiguos; `runtime.GC()` solo en tests |

Objetivos:

- Binario < 30 MB (sin modelo).
- Arranque en frío < 150 ms.
- RAM idle < 60 MB (sin modelo).
- Overhead del loop < 15 ms p95 por paso.
- Primer token < 1.5 s con modelo local caliente.

---

## 11. Testing y calidad

| Nivel | Herramientas | Cobertura objetivo |
| :--- | :--- | :--- |
| Unit | `testing`, `go-cmp`, table-driven | 85 % en `domain`, `agent`, `policy` |
| Integración | `httptest`, SQLite temporal, `t.TempDir()` | 70 % en adapters |
| E2E | binario + `--json` | Flujos críticos |
| TUI | `teatest` + golden files | Pantallas principales |
| Fuzz | `go test -fuzz` | Parsers de tool calls, paths, config |
| Race | `-race` | Todo el CI |
| Seguridad | `govulncheck`, `gosec`, `golangci-lint` | 0 high/critical |
| Evals | ver documento 11 | Umbral por release |

Reglas:

- Nada de `time.Sleep` en tests; `synctest` o relojes fake.
- Tests deterministas: sin red, sin modelo real por defecto.
- Un test que falla debe explicar qué contrato rompe.
- Golden files versionados y revisables.

---

## 12. Build y distribución

### 12.1 Flags

```bash
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --tags) -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o dist/liquidharness ./cmd/liquidharness
```

- `-trimpath` para builds reproducibles.
- `-s -w` reduce tamaño (pero conserva `pprof` si se necesita; valorar).
- Versionado por ldflags, no por `init()`.
- `GOOS/GOARCH` matrix: windows/amd64, windows/arm64, darwin/amd64, darwin/arm64,
  linux/amd64, linux/arm64.

### 12.2 Reproducibilidad

- `go mod verify`.
- GoReleaser con `mod_timestamp`, `-trimpath`, `SOURCE_DATE_EPOCH`.
- SBOM con Syft.
- Firma con cosign (keyless OIDC en CI) + checksums SHA-256.

---

## 13. Decisiones derivadas

| ID | Decisión | Motivo |
| :--- | :--- | :--- |
| G-01 | Go 1.27, un módulo, `cmd/` + `internal/` | Simplicidad y toolchain moderno |
| G-02 | Cobra para CLI | Subcomandos y completions |
| G-03 | TOML + env + flags (sin Viper) | Menos magia, config tipada |
| G-04 | Bubble Tea v2 + Lip Gloss v2 | TUI testeable y moderna |
| G-05 | HTTP propio para el modelo | Sin SDK, control de errores/streaming |
| G-06 | SQLite modernc + FTS5 + vectores BLOB | CGO=0 y persistencia robusta |
| G-07 | MCP Go SDK oficial | Conformidad y mantenimiento |
| G-08 | `slog` + JSONL de auditoría | Observabilidad sin dependencias |
| G-09 | ≤ 15 dependencias directas | Seguridad y mantenimiento |
| G-10 | GoReleaser + cosign + SBOM | Release verificable |

---

## 14. Referencias

1. Go 1.27 — release notes:
   <https://go.dev/doc/go1.27>
2. Go 1.27 — blog de release:
   <https://go.dev/blog/go1.27>
3. Go — *Organizing a Go module*:
   <https://go.dev/doc/modules/layout>
4. Go — *Standard Project Layout*:
   <https://github.com/golang-standards/project-layout>
5. Charm — *v2: Bubble Tea, Lip Gloss, Bubbles*:
   <https://charm.land/blog/v2/>
6. Bubble Tea v2 — what's new:
   <https://github.com/charmbracelet/bubbletea/discussions/1374>
7. MCP Go SDK:
   <https://github.com/modelcontextprotocol/go-sdk>
8. `modernc.org/sqlite`:
   <https://pkg.go.dev/modernc.org/sqlite>
9. SQLite FTS5:
   <https://www.sqlite.org/fts5.html>
10. `sqlite-vec`:
    <https://github.com/asg017/sqlite-vec>
11. GoReleaser:
    <https://goreleaser.com/>
12. `golang.org/x/sync`:
    <https://pkg.go.dev/golang.org/x/sync>
13. `jsonschema-go`:
    <https://github.com/google/jsonschema-go>
14. `goccy/go-llama` (experimental):
    <https://github.com/goccy/go-llama>
