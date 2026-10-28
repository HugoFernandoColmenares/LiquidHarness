# 06 — Guía de diseño TUI: componentes, tokens y variables

> **Estado:** Especificación de diseño v1.0 · **Fecha:** 2026-10-28
> **Sistema:** *Liquid Terminal Design System* (LTDS)
> **Implementación:** `internal/tui/theme`, `internal/tui/components`, `internal/tui/screens`
> **Regla:** los tokens de este documento son normativos. El código es la fuente de verdad; este
> documento se sincroniza con él y CI detecta la deriva.

---

## 1. Principios de diseño

| ID | Principio | Implicación |
| :--- | :--- | :--- |
| D-01 | **Calma operativa** | La UI no grita; el color se usa con intención, no como decoración |
| D-02 | **Transparencia** | Se ve qué tool corre, con qué args y qué devolvió |
| D-03 | **Keyboard-first** | Todo se hace sin ratón; el ratón es opcional |
| D-04 | **Divulgación progresiva** | Resumen por defecto; detalle bajo demanda (`e`) |
| D-05 | **Feedback inmediato** | < 100 ms para input; streaming visible |
| D-06 | **Seguridad visible** | Riesgo, aprobación y alcance siempre explícitos |
| D-07 | **Accesible por defecto** | `NO_COLOR`, 80 columnas, sin animaciones esenciales |
| D-08 | **Consistencia** | Un token, un significado; no hex sueltos en componentes |
| D-09 | **Densidad legible** | 1 celda de aire mínimo; nunca paredes de texto |
| D-10 | **Local-first** | Nada de UI que dependa de red para funcionar |

### 1.1 Lo que la UI nunca hace

- Ocultar que una tool destructiva se ejecutó.
- Mostrar JSON crudo como respuesta principal.
- Usar solo color para indicar estado.
- Robar el foco sin razón.
- Bloquear el terminal si el proceso muere (restaurar siempre).
- Auto-aprobar por timeout.

---

## 2. Restricciones del medio

| Restricción | Valor | Consecuencia |
| :--- | :--- | :--- |
| Unidad | 1 celda de terminal | Todo spacing es entero |
| Ancho mínimo | 80 columnas | Layout debe funcionar aquí |
| Ancho cómodo | 100–120 | Layout ideal |
| Alto mínimo | 24 filas | Header + main + composer + status |
| Fuente | Monospace del usuario | Sin control de tipografía |
| Color | 16 / 256 / truecolor | Downsampling automático |
| Unicode | Variable | Glifos con fallback ASCII |
| Ratón | Opcional | Nunca requisito |
| Redimensionado | Frecuente | Reflow, no scroll horizontal |
| Persistencia | Alt screen | Restaurar al salir |

---

## 3. Grid y layout

### 3.1 Regiones

```text
┌──────────────────────────────────────────────────────────────┐
│ Header (1)                                                   │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│ Main (flex)                                                  │
│                                                              │
├──────────────────────────────────────────────────────────────┤
│ Composer (3–5)                                               │
├──────────────────────────────────────────────────────────────┤
│ Status bar (1)                                               │
└──────────────────────────────────────────────────────────────┘
```

| Región | Alto | Contenido |
| :--- | :--- | :--- |
| Header | 1 | App, modelo, sesión, estado |
| Main | flex | Transcript / pantalla activa |
| Composer | 3–5 | Input multilínea, hints |
| Status | 1 | Modo, workspace, branch, contexto, tokens |

### 3.2 Breakpoints

| Nombre | Ancho | Layout |
| :--- | :--- | :--- |
| **S** | 80–99 | Una columna; sidebar como overlay |
| **M** | 100–119 | Una columna + status compacto |
| **L** | ≥ 120 | Main + sidebar opcional (32 cols) |

No hay breakpoint por altura; si hay < 20 filas, se oculta el header y se compacta el composer.

### 3.3 Medidas

| Token | Valor | Uso |
| :--- | ---: | :--- |
| `layout.maxContentWidth` | 100 | Máximo de texto legible |
| `layout.gutter` | 1 | Entre columnas |
| `layout.screenPadX` | 2 | Margen lateral (S: 1) |
| `layout.screenPadY` | 0 | Terminal ya tiene aire |
| `layout.panelPad` | 1 | Dentro de paneles |
| `layout.rowHeight` | 1 | Filas de lista |
| `layout.sidebarWidth` | 32 | L |
| `layout.composerMin` | 3 | Input + borde + hint |
| `layout.composerMax` | 8 | Crecimiento máximo |

---

## 4. Tokens de espacio

Base = **1 celda**. Escala tipo 4px pero en celdas:

| Token primitivo | Valor | Uso típico |
| :--- | ---: | :--- |
| `space.0` | 0 | Reset |
| `space.1` | 1 | Gap mínimo, padding interno |
| `space.2` | 2 | Padding de panel, separación de bloques |
| `space.3` | 3 | Separación de secciones |
| `space.4` | 4 | Margen de pantalla |
| `space.6` | 6 | Separación grande |
| `space.8` | 8 | Bloques vacíos / onboarding |

### 4.1 Tokens semánticos

| Token | Valor | Dónde |
| :--- | ---: | :--- |
| `space.screen.x` | 2 (S:1) | Margen lateral del contenido |
| `space.screen.y` | 0 | Sin margen vertical |
| `space.panel.pad` | 1 | Dentro de cards/paneles |
| `space.panel.gap` | 1 | Entre cards |
| `space.section.gap` | 2 | Entre secciones |
| `space.list.row` | 0 | Filas de lista (1 línea) |
| `space.list.gap` | 0 | Sin gap entre filas |
| `space.form.field` | 1 | Entre label e input |
| `space.form.group` | 2 | Entre grupos |
| `space.modal.pad` | 2 | Dentro de modales |
| `space.modal.gap` | 1 | Entre bloques del modal |
| `space.status.gap` | 2 | Entre segmentos del status |

### 4.2 Reglas

- Nunca usar valores fuera de la escala (nada de `3` si no está).
- El padding va dentro del borde; el margen va fuera.
- En S, reducir `screen.x` a 1 antes de tocar `panel.pad`.
- No usar tabs; solo espacios de layout.

---

## 5. Tokens de tipografía y énfasis

La terminal no permite tamaños. La jerarquía se construye con **peso, color, glifos y espacio**.

| Rol | Atributos | Uso |
| :--- | :--- | :--- |
| `text.display` | Bold + accent | Título de pantalla |
| `text.title` | Bold | Nombre de tool, sección |
| `text.body` | Normal | Contenido |
| `text.label` | Bold + muted | Etiquetas de campo |
| `text.caption` | Dim | Metadatos, timestamps |
| `text.code` | Normal + surface | Código inline |
| `text.numeric` | Tabular (aligned) | Tokens, duración, tamaño |
| `text.link` | Underline + accent | Enlaces |
| `text.danger` | Bold + danger | Errores |
| `text.success` | Bold + success | Confirmaciones |

### 5.1 Reglas de texto

- Ancho de línea objetivo: **≤ 100 columnas**; en S, ≤ 76.
- Párrafos separados por 1 línea en blanco.
- No justificar; alinear a la izquierda.
- No usar MAYÚSCULAS sostenidas (dificulta lectura); solo etiquetas cortas.
- Truncar con `…` (U+2026) y mantener el final significativo en paths.
- Wrap por palabras; nunca cortar a mitad de un glifo ancho.
- Código y paths en un estilo distinguible, pero sin cajas por defecto.

### 5.2 Formato de números

| Dato | Formato | Ejemplo |
| :--- | :--- | :--- |
| Tokens | `1.2k`, `18.4k` | `ctx 18.4k / 32k` |
| Bytes | `1.4 KB`, `2.1 MB` | `2.1 MB` |
| Duración | `12 ms`, `1.2 s`, `1m 04s` | `1.2 s` |
| Porcentaje | Entero | `18%` |
| Fecha | ISO corta local | `2026-10-28 12:04` |

---

## 6. Sistema de color

### 6.1 Paleta primitiva — tema `liquid-dark` (default)

| Token | Hex | Uso |
| :--- | :--- | :--- |
| `ink.950` | `#070B10` | Fondo profundo |
| `ink.900` | `#0B0F14` | Canvas |
| `ink.850` | `#111823` | Superficie |
| `ink.800` | `#16202D` | Superficie elevada |
| `ink.700` | `#243244` | Borde |
| `ink.600` | `#35506B` | Borde fuerte |
| `slate.400` | `#6B7C8F` | Texto tenue |
| `slate.300` | `#9FB0C3` | Texto muted |
| `slate.100` | `#E6EDF3` | Texto principal |
| `cyan.400` | `#35C2E8` | Acento |
| `cyan.300` | `#7BE0FF` | Acento brillante |
| `cyan.700` | `#1B6F86` | Acento tenue |
| `violet.400` | `#C084FC` | Memoria |
| `green.400` | `#4ADE80` | Éxito |
| `amber.400` | `#FBBF24` | Advertencia |
| `red.400` | `#F87171` | Peligro |
| `blue.400` | `#60A5FA` | Información |

### 6.2 Paleta primitiva — tema `liquid-light`

| Token | Hex | Uso |
| :--- | :--- | :--- |
| `paper.50` | `#FFFFFF` | Superficie |
| `paper.100` | `#F7F9FC` | Canvas |
| `paper.200` | `#EEF2F7` | Superficie elevada |
| `slate.200` | `#D7E0EA` | Borde |
| `slate.500` | `#7A8898` | Texto tenue |
| `slate.600` | `#526273` | Texto muted |
| `slate.900` | `#0F1720` | Texto principal |
| `teal.700` | `#007A99` | Acento |
| `teal.800` | `#005F78` | Acento fuerte |
| `green.700` | `#15803D` | Éxito |
| `amber.700` | `#B45309` | Advertencia |
| `red.700` | `#B91C1C` | Peligro |
| `blue.700` | `#1D4ED8` | Información |
| `violet.700` | `#7E22CE` | Memoria |

### 6.3 Tokens semánticos

| Token | Dark | Light | Uso |
| :--- | :--- | :--- | :--- |
| `bg.canvas` | `ink.900` | `paper.100` | Fondo |
| `bg.surface` | `ink.850` | `paper.50` | Paneles |
| `bg.surfaceAlt` | `ink.800` | `paper.200` | Filas alternas, código |
| `bg.overlay` | `ink.950` | `paper.50` | Modales |
| `bg.selection` | `cyan.700` | `#CCF2FA` | Selección |
| `fg.default` | `slate.100` | `slate.900` | Texto |
| `fg.muted` | `slate.300` | `slate.600` | Secundario |
| `fg.faint` | `slate.400` | `slate.500` | Metadatos |
| `fg.onAccent` | `ink.950` | `paper.50` | Texto sobre acento |
| `border.default` | `ink.700` | `slate.200` | Bordes |
| `border.strong` | `ink.600` | `slate.500` | Énfasis |
| `border.focus` | `cyan.400` | `teal.700` | Foco |
| `accent.default` | `cyan.400` | `teal.700` | Acción |
| `accent.bright` | `cyan.300` | `teal.800` | Hover/streaming |
| `accent.dim` | `cyan.700` | `#B3E6F2` | Fondos de acento |
| `state.success` | `green.400` | `green.700` | OK |
| `state.warning` | `amber.400` | `amber.700` | Advertencia |
| `state.danger` | `red.400` | `red.700` | Error/peligro |
| `state.info` | `blue.400` | `blue.700` | Info |
| `state.memory` | `violet.400` | `violet.700` | Memoria |
| `diff.add` | `green.400` | `green.700` | Línea añadida |
| `diff.del` | `red.400` | `red.700` | Línea borrada |
| `diff.addBg` | `#0F2A1A` | `#DCFCE7` | Fondo add |
| `diff.delBg` | `#2A1414` | `#FEE2E2` | Fondo del |
| `syntax.keyword` | `violet.400` | `violet.700` | Código |
| `syntax.string` | `green.400` | `green.700` | Código |
| `syntax.number` | `amber.400` | `amber.700` | Código |
| `syntax.comment` | `slate.400` | `slate.500` | Código |

### 6.4 Contraste

Objetivos WCAG 2.2 AA:

- Texto normal: **≥ 4.5:1**.
- Texto grande/bold: **≥ 3:1**.
- Bordes/foco: **≥ 3:1**.
- Nunca usar color como único indicador.

Verificación (aproximada, a validar con herramienta):

| Par | Ratio | Cumple |
| :--- | ---: | :---: |
| `slate.100` sobre `ink.900` | ~15.8:1 | ✅ |
| `slate.300` sobre `ink.900` | ~8.1:1 | ✅ |
| `slate.400` sobre `ink.900` | ~4.6:1 | ✅ (texto tenue) |
| `cyan.400` sobre `ink.900` | ~9.2:1 | ✅ |
| `red.400` sobre `ink.900` | ~6.9:1 | ✅ |
| `slate.900` sobre `paper.100` | ~16.9:1 | ✅ |
| `slate.600` sobre `paper.100` | ~7.2:1 | ✅ |
| `teal.700` sobre `paper.50` | ~5.4:1 | ✅ |
| `slate.200` (borde) sobre `paper.50` | ~1.4:1 | ❌ decorativo; foco usa `teal.700` |

Los bordes decorativos no necesitan 3:1; los interactivos sí.

### 6.5 ANSI 16 y 256

| Semántico | ANSI 16 | 256 |
| :--- | :--- | ---: |
| `fg.default` | bright white | 255 |
| `fg.muted` | white | 250 |
| `fg.faint` | bright black | 244 |
| `accent.default` | bright cyan | 45 |
| `accent.bright` | bright cyan | 51 |
| `state.success` | bright green | 84 |
| `state.warning` | bright yellow | 214 |
| `state.danger` | bright red | 203 |
| `state.info` | bright blue | 75 |
| `state.memory` | bright magenta | 177 |
| `bg.canvas` | black | 232 |
| `bg.surface` | black | 234 |
| `border.default` | black | 238 |

### 6.6 `NO_COLOR` y modos degradados

| Entorno | Comportamiento |
| :--- | :--- |
| `NO_COLOR=1` | Sin color; conservar bold/dim/underline |
| `TERM=dumb` | Sin ANSI; texto plano, sin alt screen |
| `--plain` | Sin bordes, sin glifos Unicode, sin animaciones |
| 16 colores | Mapear a ANSI 16 |
| Sin TTY | Forzar headless; nunca TUI |

Regla: la información **nunca** depende del color. Siempre hay glifo o palabra (`✓ ok`, `✗ error`).

---

## 7. Glifos e iconografía

| Concepto | Unicode | ASCII fallback |
| :--- | :---: | :---: |
| Usuario | `›` | `>` |
| Asistente | `◆` | `*` |
| Razonamiento | `◇` | `~` |
| Tool | `⚙` | `#` |
| Éxito | `✓` | `+` |
| Error | `✗` | `x` |
| Advertencia | `!` | `!` |
| Aprobación | `⚠` | `!` |
| Memoria | `✦` | `m` |
| RAG | `⌕` | `?` |
| En curso | `◐` | `-` |
| Flecha | `→` | `->` |
| Separador | `·` | `|` |
| Colapsado | `▸` | `>` |
| Expandido | `▾` | `v` |
| Borde | `│ ─ ┌ ┐ └ ┘` | `| - + + + +` |

- Nerd Font no es requisito.
- Detectar soporte Unicode; si no, `--ascii`.
- No usar emoji en UI funcional (ancho variable, rompe layout).

---

## 8. Bordes, elevación y foco

| Token | Valor | Uso |
| :--- | :--- | :--- |
| `border.width` | 1 | Único grosor |
| `border.style.default` | single | Paneles |
| `border.style.focus` | double o accent | Panel activo |
| `border.style.overlay` | rounded | Modales |
| `radius` | 0 | La terminal no tiene radio |
| `elevation.overlay` | `bg.overlay` + borde | Modales |
| `elevation.card` | `bg.surface` + borde | Cards |

**Foco:** barra izquierda de 1 celda en `border.focus` + título bold. No usar solo cambio de color de
fondo (poco visible en algunos terminales).

---

## 9. Movimiento

| Token | Valor | Uso |
| :--- | ---: | :--- |
| `motion.instant` | 0 ms | Cambios de estado |
| `motion.fast` | 80 ms | Aparición de toast |
| `motion.spinner` | 100 ms | Frame de spinner |
| `motion.cursor` | 500 ms | Parpadeo del cursor de streaming |
| `motion.toast` | 3000 ms | Duración del toast |
| `motion.approvalTimeout` | 300000 ms | Deny por timeout |

- Sin transiciones de pantalla.
- `--no-animations` o `ui.animations=false` desactiva spinner (usa `...`).
- Respetar `TERM=dumb`.

---

## 10. Catálogo de componentes

### 10.1 Resumen

| # | Componente | Archivo | Prioridad |
| :-: | :--- | :--- | :--- |
| 1 | App shell | `components/shell.go` | P0 |
| 2 | Header | `components/header.go` | P0 |
| 3 | Status bar | `components/statusbar.go` | P0 |
| 4 | Transcript | `components/transcript.go` | P0 |
| 5 | Message | `components/message.go` | P0 |
| 6 | Reasoning block | `components/reasoning.go` | P1 |
| 7 | Tool card | `components/toolcard.go` | P0 |
| 8 | Diff viewer | `components/diff.go` | P1 |
| 9 | Approval modal | `components/approval.go` | P0 |
| 10 | Composer | `components/composer.go` | P0 |
| 11 | Command palette | `components/palette.go` | P1 |
| 12 | Toast | `components/toast.go` | P1 |
| 13 | Modal base | `components/modal.go` | P0 |
| 14 | List | `components/list.go` | P0 |
| 15 | Table | `components/table.go` | P1 |
| 16 | Empty state | `components/empty.go` | P1 |
| 17 | Error banner | `components/error.go` | P0 |
| 18 | Progress | `components/progress.go` | P1 |
| 19 | Memory inspector | `screens/memory.go` | P2 |
| 20 | Eval dashboard | `screens/eval.go` | P2 |
| 21 | Wizard | `screens/wizard.go` | P0 |
| 22 | Help overlay | `components/help.go` | P1 |

### 10.2 App shell

**Anatomía:** header + main + composer + status.  
**Estados:** normal, modal (overlay), palette (overlay), approval (overlay), error (banner).  
**Tokens:** `bg.canvas`, `layout.screen.x`, `border.default`.  
**Reglas:** el overlay captura teclado; `Esc` cierra; el foco vuelve al origen.

### 10.3 Header

```text
┌ LiquidHarness ─ LFM2.5-2.6B ─ session 7f3a ──────────────── 12:04 ┐
```

| Elemento | Token | Ejemplo |
| :--- | :--- | :--- |
| App | `text.title` + accent | LiquidHarness |
| Modelo | `text.caption` | LFM2.5-2.6B |
| Sesión | `text.caption` | session 7f3a |
| Estado | glifo + color | `◐ thinking`, `✓ ready`, `✗ error` |
| Hora | `text.caption` | 12:04 |

En S: solo app + estado.

### 10.4 Status bar

```text
 ask · ~/project · main* · ctx 18% · ↑2.1k ↓340 · 3 tools · ? help
```

| Segmento | Fuente | Token |
| :--- | :--- | :--- |
| Modo | policy | `state.warning` si no es ask |
| Workspace | cwd | `fg.muted` |
| Git | branch + dirty | `fg.muted` |
| Contexto | % usado | `state.warning` > 75 % |
| Tokens | in/out | `fg.faint` |
| Tools | count del turno | `fg.faint` |
| Ayuda | `?` | `accent` |

Ocultar segmentos si el ancho < 100, en orden: tools → tokens → git.

### 10.5 Transcript

**Anatomía:** lista virtualizada de bloques (`message`, `tool`, `reasoning`, `error`, `divider`).  
**Estados:** vacío, streaming, scrolled, searching.  
**Reglas:**

- Auto-scroll si el usuario está al fondo; si no, mostrar badge "nuevos ↓".
- `g`/`G` inicio/fin; `j`/`k` línea; `PgUp`/`PgDn`.
- `e` expande el bloque enfocado; `y` copia; `o` abre en `$EDITOR`.
- No re-renderizar todo por token; append incremental.

### 10.6 Message

```text
◆ assistant
  The notes define three decisions: local-first, SQLite, and a single binary.
```

| Rol | Glifo | Color | Estilo |
| :--- | :---: | :--- | :--- |
| Usuario | `›` | `accent` | Bold |
| Asistente | `◆` | `fg.default` | Normal |
| Sistema | `·` | `fg.faint` | Dim |
| Tool | `⚙` | según riesgo | Card |
| Error | `✗` | `state.danger` | Bold |

- El cuerpo va indentado 2 celdas.
- Sin burbujas ni cajas por defecto (ruido visual).
- Código inline con `bg.surfaceAlt`.
- Enlaces subrayados; `o` abre.

### 10.7 Reasoning block

```text
◇ reasoning · 1.2 s · 412 tokens                              [▸ show]
```

- Colapsado por defecto.
- `e` expande; `c` copia.
- Color `fg.faint`; nunca se mezcla con la respuesta.
- Si el modelo no emite ` thinking`, no se muestra.

### 10.8 Tool card

```text
⚙ fs_edit  R1  ✓ 12 ms                                           [▾]
  path="docs/notes.md" · 2 edits · +18 −4
  ┌ diff ──────────────────────────────────────────────────────┐
  │ 12  - old decision: use bbolt                               │
  │ 12  + old decision: use SQLite                              │
  └─────────────────────────────────────────────────────────────┘
```

| Estado | Glifo | Color |
| :--- | :---: | :--- |
| Pending | `·` | `fg.faint` |
| Running | `◐` | `accent` |
| OK | `✓` | `state.success` |
| Error | `✗` | `state.danger` |
| Denied | `⊘` | `state.warning` |
| Truncated | `…` | `state.warning` |

Anatomía: glifo + nombre + badge de riesgo + estado + duración + resumen + expandible.
Riesgo: `R0` muted, `R1` info, `R2` warning, `R3+` danger.
`e` expande; `y` copia args; `o` abre el path.

### 10.9 Diff viewer

```text
 10  context line
 11 -removed
 11 +added
```

- Números de línea a la derecha o izquierda, `fg.faint`.
- `+` con `diff.add`/`diff.addBg`; `-` con `diff.del`/`diff.delBg`.
- Contexto 3 líneas; hunks colapsables.
- En `NO_COLOR`: prefijos `+`/`-` y sin fondo.
- Ancho: wrap en S, scroll horizontal solo en código (con indicador).

### 10.10 Approval modal

```text
┌ Approval required ──────────────────────────────────────────────┐
│ ⚠ R2 · destructive                                              │
│ fs_delete                                                       │
│ Delete "docs/old/" (3 files, 12.4 KB)                           │
│                                                                 │
│ - docs/old/a.md                                                 │
│ - docs/old/b.md                                                 │
│ - docs/old/c.md                                                 │
│                                                                 │
│ Moves to .liquidharness/trash/2026-10-28T12-04-01/              │
│                                                                 │
│ [a] allow once   [s] allow session   [d] deny   [Esc] deny      │
└─────────────────────────────────────────────────────────────────┘
```

Reglas:

- Foco inicial en **deny** para R2+.
- `a`/`s`/`d` explícitos; `Enter` ejecuta la opción enfocada.
- El alcance se muestra siempre.
- Timeout 5 min → deny.
- No se puede cerrar con click fuera (evita accidentes).

### 10.11 Composer

```text
› Ask anything…  @file  /command                            0/32k  ◐
```

| Estado | Borde | Hint |
| :--- | :--- | :--- |
| Idle | `border.default` | `@file /command` |
| Focus | `border.focus` | igual |
| Streaming | `accent.dim` | `Esc to cancel` |
| Error | `state.danger` | mensaje |
| Disabled | `fg.faint` | `model offline` |

- Multilínea con `Shift+Enter` o `Ctrl+J`.
- Historial con `↑`/`↓` (solo si el cursor está en la primera/última línea).
- `@` abre file picker; `/` abre palette de comandos.
- Paste: preservar saltos, limitar a 64 KiB, mostrar "pasted 42 lines".
- Contador de tokens del prompt (estimado) a la derecha.

### 10.12 Command palette

```text
┌ Command palette ────────────────────────────────────────────────┐
│ › rag                                                           │
│ ─────────────────────────────────────────────────────────────── │
│ ▸ rag index          Index a folder into the local vector store │
│   rag search         Search indexed documents                   │
│   rag stats          Show index size and health                 │
│   session export     Export the current session as Markdown     │
└─────────────────────────────────────────────────────────────────┘
```

- Fuzzy match en nombre + descripción.
- Grupos: Session, Model, RAG, Memory, Eval, Config, Help.
- `↑`/`↓`, `Enter`, `Esc`.
- Muestra el keybinding a la derecha si existe.

### 10.13 Toast

```text
✓ Indexed 42 files · 318 chunks · 1.4 s
```

- Esquina inferior derecha (o bajo el status en S).
- 3 s; `Esc` descarta; máx. 3 apilados.
- Tipos: success, warning, danger, info.
- No usar para errores críticos (usar banner).

### 10.14 Error banner

```text
┌ ✗ Model offline ────────────────────────────────────────────────┐
│ Cannot reach http://127.0.0.1:8080/v1.                          │
│ Start it with: liquidharness models serve                       │
│ [r] retry   [d] doctor   [Esc] dismiss                          │
└─────────────────────────────────────────────────────────────────┘
```

- Persistente hasta resolver o descartar.
- Qué pasó, por qué, qué hacer.
- Nunca stack traces.

### 10.15 List y table

- List: selección con `bg.selection` + barra izquierda; `j/k`, `g/G`, `/` filter.
- Table: columnas alineadas, header bold, sort con `s`, sin bordes verticales.
- En S: convertir tabla en lista de pares clave-valor.

### 10.16 Empty state

```text
  ◆  No sessions yet
     Press Ctrl+N to start, or run: liquidharness ask "summarize docs/"
```

- Un glifo, un título, una acción.
- Sin ilustraciones ASCII grandes.

### 10.17 Progress

```text
Downloading LFM2.5-2.6B Q5_K_M
[██████████████░░░░░░░░░░]  58%  1.2/2.0 GB  8.4 MB/s  ETA 1m 32s
```

- Barra con `█`/`░`; ASCII: `#`/`-`.
- Mostrar bytes, velocidad y ETA.
- Cancelable con `Esc`.

### 10.18 Wizard

```text
┌ Welcome to LiquidHarness ───────────────────────────────────────┐
│ Step 1/4 · Hardware                                             │
│                                                                 │
│ Detected: 16 GB RAM · 8 cores · no GPU                          │
│                                                                 │
│ Recommended profile:                                            │
│  (•) Balanced  LFM2.5-2.6B Q5_K_M · ~2.0 GB · 32K context       │
│  ( ) Fast      LFM2.5-2.6B Q4_K_M · ~1.7 GB · 16K context       │
│  ( ) Quality   LFM2.5-8B-A1B Q4_K_M · ~5.0 GB · 32K context     │
│  ( ) Remote    Connect to an existing OpenAI-compatible server  │
│                                                                 │
│ [Enter] continue   [q] quit                                     │
└─────────────────────────────────────────────────────────────────┘
```

Pasos: Hardware → Runtime → Modelo → Verificación. Barra de progreso de pasos en el título.

### 10.19 Help overlay

```text
┌ Help ───────────────────────────────────────────────────────────┐
│ Global          Composer            Transcript                  │
│ ? help          Enter send          j/k scroll                  │
│ Ctrl+K palette  Shift+Enter newline e expand                    │
│ Ctrl+N new      @ file             y copy                      │
│ Esc cancel      / command          o open                      │
└─────────────────────────────────────────────────────────────────┘
```

Generado desde el keymap, no hardcodeado.

---

## 11. Keybindings

### 11.1 Global

| Tecla | Acción | Contexto |
| :--- | :--- | :--- |
| `?` | Ayuda | Siempre (salvo input) |
| `Ctrl+K` | Command palette | Siempre |
| `Ctrl+P` | Sessions | Siempre |
| `Ctrl+N` | Nueva sesión | Siempre |
| `Ctrl+R` | Reintentar último turno | Si hay error |
| `Esc` | Cancelar turno / cerrar overlay | Contextual |
| `Ctrl+C` | Cancelar; doble < 2 s → salir | Siempre |
| `Ctrl+L` | Limpiar pantalla | Siempre |
| `Ctrl+E` | Expandir/colapsar bloque | Transcript |
| `Tab` / `Shift+Tab` | Siguiente/anterior foco | Siempre |
| `Alt+1..5` | Chat, Sessions, Memory, RAG, Eval | Siempre |
| `q` | Salir | Solo si no hay input |

### 11.2 Composer

| Tecla | Acción |
| :--- | :--- |
| `Enter` | Enviar |
| `Shift+Enter` / `Ctrl+J` | Nueva línea |
| `↑` / `↓` | Historial (en bordes) |
| `@` | File picker |
| `/` | Command palette (comandos) |
| `Ctrl+V` | Pegar |
| `Ctrl+O` | Abrir `$EDITOR` |
| `Ctrl+U` | Borrar línea |
| `Ctrl+W` | Borrar palabra |

### 11.3 Transcript

| Tecla | Acción |
| :--- | :--- |
| `j` / `k` | Abajo / arriba |
| `PgUp` / `PgDn` | Página |
| `g` / `G` | Inicio / fin |
| `e` | Expandir |
| `y` | Copiar bloque |
| `o` | Abrir path/enlace |
| `/` | Buscar en transcript |
| `n` / `N` | Siguiente/anterior match |

### 11.4 Approval

| Tecla | Acción |
| :--- | :--- |
| `a` | Allow once |
| `s` | Allow session |
| `d` | Deny |
| `Esc` | Deny |
| `Enter` | Opción enfocada |
| `Tab` | Cambiar foco |

### 11.5 Reglas de conflicto

- El overlay captura todo excepto `Ctrl+C`.
- `Esc` cierra el overlay, no la app.
- `q` no sale si el foco está en un input.
- Las teclas se documentan en `keys.go` y se muestran en la UI.

---

## 12. Pantallas y wireframes

### 12.1 Chat (L, 120×36)

```text
┌ LiquidHarness ─ LFM2.5-2.6B ─ session 7f3a ──────────────────────────── 12:04 ┐
│                                                                              │
│  › summarize the decisions in docs/notes.md                                  │
│                                                                              │
│  ◆ assistant                                                                 │
│    I'll inspect the folder and read the notes.                               │
│                                                                              │
│  ⚙ fs_list  R0  ✓ 12 ms                                                [▾]  │
│    path="docs" depth=1 → 4 files                                             │
│                                                                              │
│  ⚙ fs_read  R0  ✓ 8 ms                                                 [▾]  │
│    path="docs/notes.md" → 42 lines · 2.1 KB                                  │
│                                                                              │
│  ◆ assistant                                                                 │
│    The notes define three decisions:                                         │
│    1. Local-first runtime with no cloud dependency.                          │
│    2. SQLite for state and RAG.                                              │
│    3. A single binary for CLI and TUI.                                       │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ › Ask anything…  @file  /command                                  18.4k/32k │
├──────────────────────────────────────────────────────────────────────────────┤
│ ask · ~/project · main* · ctx 57% · ↑2.1k ↓340 · 2 tools · ? help            │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 12.2 Approval (M, 100×30)

```text
┌ Approval required ──────────────────────────────────────────────────────────┐
│ ⚠ R2 · destructive                                                          │
│ fs_delete                                                                   │
│ Delete "docs/old/" (3 files, 12.4 KB)                                       │
│                                                                             │
│ - docs/old/a.md                                                             │
│ - docs/old/b.md                                                             │
│ - docs/old/c.md                                                             │
│                                                                             │
│ Moves to .liquidharness/trash/2026-10-28T12-04-01/                          │
│                                                                             │
│ [a] allow once   [s] allow session   [d] deny   [Esc] deny                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.3 Command palette (M, 100×30)

```text
┌ Command palette ────────────────────────────────────────────────────────────┐
│ › rag                                                                       │
│ ─────────────────────────────────────────────────────────────────────────── │
│ ▸ rag index          Index a folder into the local vector store             │
│   rag search         Search indexed documents                               │
│   rag stats          Show index size and health                             │
│   session export     Export the current session as Markdown                 │
│   memory inspect     Browse and edit long-term memory                       │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.4 Sessions (L, 120×36)

```text
┌ Sessions ───────────────────────────────────────────────────────────────────┐
│ / search                                                                    │
│ ─────────────────────────────────────────────────────────────────────────── │
│ ▸ 7f3a  Summarize docs/notes.md          2m ago   12 msgs   3.1k tokens     │
│   2b91  Index the handbook folder        1h ago   48 msgs  12.4k tokens     │
│   c04e  Remember my writing preferences  3d ago    8 msgs   1.2k tokens     │
│                                                                             │
│ [Enter] resume  [d] delete  [e] export  [Esc] back                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.5 Eval dashboard (L, 120×36)

```text
┌ Evaluations ─ core-v3 ──────────────────────────────────────────────────────┐
│ Run         Model             Pass   Tool   Args   p95     Status           │
│ 2026-10-27  local/LFM2.5-2.6B 84.0%  91.2%  87.5%  1.2s    ✓ pass           │
│ 2026-10-20  local/LFM2.5-2.6B 82.0%  90.8%  86.9%  1.3s    ✓ pass           │
│ 2026-10-13  local/LFM2.5-2.6B 79.0%  88.1%  84.0%  1.4s    ✗ -3.0%          │
│                                                                             │
│ Failures by tag: tool-args 6 · context 3 · policy 1 · timeout 0             │
│                                                                             │
│ [Enter] details  [c] compare  [r] rerun  [Esc] back                         │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.6 Memory inspector (L, 120×36)

```text
┌ Memory ─ user.preference ───────────────────────────────────────────────────┐
│ / search                                           12 items · 3 namespaces  │
│ ─────────────────────────────────────────────────────────────────────────── │
│ ▸ writing.style        Prefers concise, direct prose        0.92  2d ago    │
│   writing.language     Writes documentation in Spanish      0.88  2d ago    │
│   project.stack        Go 1.27, SQLite, Bubble Tea          0.95  1h ago    │
│                                                                             │
│ [Enter] edit  [d] forget  [e] export  [Esc] back                            │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 12.7 Error (S, 80×24)

```text
┌ ✗ Model offline ────────────────────────────────────────────────────────────┐
│ Cannot reach http://127.0.0.1:8080/v1.                                      │
│ Start it with: liquidharness models serve                                   │
│                                                                             │
│ [r] retry   [d] doctor   [Esc] dismiss                                      │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 13. Accesibilidad

| Requisito | Implementación |
| :--- | :--- |
| Contraste | Tokens verificados ≥ 4.5:1 (texto) |
| No color-only | Glifo + palabra + color |
| `NO_COLOR` | Soportado; conservar énfasis |
| `TERM=dumb` | Modo plano, sin alt screen |
| Ancho 80 | Layout S probado |
| Fuentes | Sin Nerd Font; ASCII fallback |
| Movimiento | `--no-animations` |
| Screen readers | `--plain` + salida lineal; evitar artefactos |
| Teclado | Todo accesible sin ratón |
| Timeout | Aprobaciones no expiran a allow |
| Idioma | Textos externalizados; no concatenar |
| Unicode | Medir ancho con `runewidth`; no asumir 1 celda |

### 13.1 Pruebas de accesibilidad

- [ ] Golden view a 80×24 con `NO_COLOR=1`.
- [ ] Golden view con `TERM=dumb`.
- [ ] Navegación completa solo con teclado.
- [ ] Contraste calculado en CI para tokens.
- [ ] Ningún estado depende solo del color.
- [ ] Textos de error con "qué/por qué/qué hacer".

---

## 14. Contenido y microcopy

### 14.1 Tono

- Directo, sin humor en errores.
- Sin jerga innecesaria.
- En el idioma del usuario; por defecto inglés en CLI, localizado en TUI.

### 14.2 Errores

Estructura: **qué pasó → por qué → qué hacer**.

```text
✗ Cannot write docs/notes.md
  The file is read-only (mode 0444).
  Run: chmod u+w docs/notes.md, or choose another path.
```

### 14.3 Confirmaciones

- Verbo + objeto + consecuencia.
- "Delete 3 files? They will be moved to trash." no "Are you sure?".

### 14.4 Tool summaries

- `fs_read` → `path="..." → 42 lines · 2.1 KB`
- `fs_edit` → `path="..." · 2 edits · +18 −4`
- `fs_delete` → `3 files · moved to trash`
- `rag_search` → `6 chunks · 3 sources`
- `memory_add` → `writing.style · confidence 0.92`

Nunca mostrar JSON crudo por defecto; `e` lo muestra.

---

## 15. Implementación de tokens en Go

### 15.1 Tipos

```go
package theme

type Color string

type Palette struct {
	BgCanvas     Color
	BgSurface    Color
	BgSurfaceAlt Color
	BgOverlay    Color
	BgSelection  Color
	FgDefault    Color
	FgMuted      Color
	FgFaint      Color
	FgOnAccent   Color
	Border       Color
	BorderStrong Color
	BorderFocus  Color
	Accent       Color
	AccentBright Color
	AccentDim    Color
	Success      Color
	Warning      Color
	Danger       Color
	Info         Color
	Memory       Color
	DiffAdd      Color
	DiffDel      Color
	DiffAddBg    Color
	DiffDelBg    Color
}

type Theme struct {
	Name    string
	Dark    bool
	Palette Palette
}
```

### 15.2 Tokens

```go
var LiquidDark = Theme{
	Name: "liquid-dark",
	Dark: true,
	Palette: Palette{
		BgCanvas:     "#0B0F14",
		BgSurface:    "#111823",
		BgSurfaceAlt: "#16202D",
		BgOverlay:    "#070B10",
		BgSelection:  "#1B6F86",
		FgDefault:    "#E6EDF3",
		FgMuted:      "#9FB0C3",
		FgFaint:      "#6B7C8F",
		FgOnAccent:   "#070B10",
		Border:       "#243244",
		BorderStrong: "#35506B",
		BorderFocus:  "#35C2E8",
		Accent:       "#35C2E8",
		AccentBright: "#7BE0FF",
		AccentDim:    "#1B6F86",
		Success:      "#4ADE80",
		Warning:      "#FBBF24",
		Danger:       "#F87171",
		Info:         "#60A5FA",
		Memory:       "#C084FC",
		DiffAdd:      "#4ADE80",
		DiffDel:      "#F87171",
		DiffAddBg:    "#0F2A1A",
		DiffDelBg:    "#2A1414",
	},
}

var LiquidLight = Theme{
	Name: "liquid-light",
	Dark: false,
	Palette: Palette{
		BgCanvas:     "#F7F9FC",
		BgSurface:    "#FFFFFF",
		BgSurfaceAlt: "#EEF2F7",
		BgOverlay:    "#FFFFFF",
		BgSelection:  "#CCF2FA",
		FgDefault:    "#0F1720",
		FgMuted:      "#526273",
		FgFaint:      "#7A8898",
		FgOnAccent:   "#FFFFFF",
		Border:       "#D7E0EA",
		BorderStrong: "#7A8898",
		BorderFocus:  "#007A99",
		Accent:       "#007A99",
		AccentBright: "#005F78",
		AccentDim:    "#B3E6F2",
		Success:      "#15803D",
		Warning:      "#B45309",
		Danger:       "#B91C1C",
		Info:         "#1D4ED8",
		Memory:       "#7E22CE",
		DiffAdd:      "#15803D",
		DiffDel:      "#B91C1C",
		DiffAddBg:    "#DCFCE7",
		DiffDelBg:    "#FEE2E2",
	},
}
```

### 15.3 Resolución

```go
type Options struct {
	NoColor    bool
	Plain      bool
	Animations bool
	Profile    colorprofile.Profile
}

func Resolve(opts Options) Theme {
	t := LiquidDark
	if !opts.NoColor && !opts.Plain {
		// detección de fondo claro vía Bubble Tea (tea.BackgroundColorMsg)
	}
	return t
}
```

- La detección de tema claro/oscuro la hace Bubble Tea (`tea.BackgroundColorMsg`).
- El downsampling lo hace `colorprofile` / Bubble Tea v2.
- `NO_COLOR` no elimina bold/dim/underline.
- Los estilos se derivan con `lipgloss.NewStyle().Foreground(...)`.

### 15.4 Estilos

```go
func Styles(t Theme) Styles {
	return Styles{
		Title:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Palette.FgDefault)),
		Muted:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Palette.FgMuted)),
		Faint:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Palette.FgFaint)),
		Accent:  lipgloss.NewStyle().Foreground(lipgloss.Color(t.Palette.Accent)),
		Danger:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Palette.Danger)),
		Success: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(t.Palette.Success)),
		Border:  lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color(t.Palette.Border)),
		Focus:   lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color(t.Palette.BorderFocus)),
	}
}
```

> Verificar la API exacta de Lip Gloss v2 al implementar (`charm.land/lipgloss/v2`); los nombres de
> tipos pueden cambiar respecto a v1.

### 15.5 Deriva de tokens

CI ejecuta un test que:

1. Parsea este documento (tablas de tokens).
2. Compara con `theme.LiquidDark`/`LiquidLight`.
3. Falla si hay diferencias no documentadas.

Alternativa más simple: generar las tablas del documento desde el código en `make docs`.

---

## 16. Inventario de capturas (`docs/screenshots/`)

### 16.1 Convención

```text
NN-area--state--WxH.png
```

Ejemplos:

```text
01-chat--idle--120x36.png
02-chat--streaming--120x36.png
03-toolcard--expanded--120x36.png
04-approval--delete--100x30.png
05-palette--commands--100x30.png
06-sessions--list--120x36.png
07-memory--inspector--120x36.png
08-rag--citations--120x36.png
09-eval--dashboard--120x36.png
10-wizard--profile--100x30.png
11-error--banner--80x24.png
12-nocolor--chat--80x24.png
13-light--chat--120x36.png
14-help--overlay--100x30.png
```

### 16.2 Requisitos

| Requisito | Regla |
| :--- | :--- |
| Determinismo | Datos sintéticos, sin fechas relativas |
| Privacidad | Sin paths personales, tokens ni emails |
| Tamaño | Exacto al viewport indicado |
| Fuente | Monospace estándar (JetBrains Mono / SF Mono) |
| Tema | `liquid-dark` salvo que el nombre diga `light` |
| Color | Truecolor; incluir una captura `nocolor` |
| Formato | PNG optimizado, < 300 KB |
| Herramienta | VHS o `tmux capture-pane` documentado |

### 16.3 Checklist de captura

- [ ] La captura corresponde al estado del nombre.
- [ ] No hay información sensible.
- [ ] El layout no está roto (bordes alineados).
- [ ] Los estados tienen glifo además de color.
- [ ] La captura se referencia en este documento o en el README.
- [ ] Se actualiza cuando cambia el componente.

---

## 17. Checklist de revisión de diseño

- [ ] ¿Funciona a 80×24?
- [ ] ¿Funciona con `NO_COLOR` y `TERM=dumb`?
- [ ] ¿Todo estado tiene glifo + texto, no solo color?
- [ ] ¿El foco es visible sin color?
- [ ] ¿Los errores dicen qué/por qué/qué hacer?
- [ ] ¿Las acciones destructivas muestran consecuencia y alcance?
- [ ] ¿El texto respeta 100 columnas?
- [ ] ¿Los números están alineados?
- [ ] ¿No hay JSON crudo por defecto?
- [ ] ¿Los tokens usados existen en la escala?
- [ ] ¿La pantalla tiene golden test?
- [ ] ¿Hay screenshot actualizado?

---

## 18. Referencias

1. Material Design 3 — Design tokens:
   <https://m3.material.io/foundations/design-tokens>
2. WCAG 2.2 — Contrast Minimum:
   <https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html>
3. Charm — Bubble Tea v2:
   <https://github.com/charmbracelet/bubbletea>
4. Charm — Lip Gloss:
   <https://github.com/charmbracelet/lipgloss>
5. Charm — Bubbles:
   <https://github.com/charmbracelet/bubbles>
6. `NO_COLOR`:
   <https://no-color.org/>
7. Command Line Interface Guidelines:
   <https://clig.dev/>
8. VHS — grabaciones de terminal:
   <https://github.com/charmbracelet/vhs>
