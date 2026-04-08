# Auditoría de rendimiento — `go_backend`

**Fecha:** 2026-04-07  
**Entorno:** darwin/arm64, Go 1.26.1, Apple M4  
**Alcance:** módulo `github.com/siigofiscal/go_backend` (tests unitarios + un benchmark de referencia).

---

## 1. Resumen ejecutivo

| Área | Herramienta | Resultado |
|------|-------------|-----------|
| Condiciones de carrera | `go test -race ./...` | Sin hallazgos |
| Análisis estático | `go vet ./...` | Sin hallazgos |
| Contención de mutex / bloqueos | `-mutexprofile`, `-blockprofile` (paquete `internal/handler`) | Perfiles de muy baja señal; bloqueo dominante en `testing.(*T).Parallel` / `chanrecv` (infraestructura de tests), no en lógica de negocio |
| CPU (muestras útiles) | `go test -bench=… -cpuprofile` | Perfil con ~740 ms de muestras en benchmark `filter` |
| Memoria (heap) | `-memprofile` + `go tool pprof` | Gráficos SVG generados bajo `perf_audit/` |

**Nota sobre deadlocks:** el runtime de Go aborta con `fatal error: all goroutines are asleep - deadlock!` cuando ninguna goroutine puede progresar. No hay un analizador estático oficial de “orden de bloqueo” como en otros lenguajes. La práctica recomendada es: detector de carreras, perfiles de mutex/bloqueo, revisiones de código en zonas con `sync.Mutex` / `RWMutex`, y pruebas de carga con `net/http/pprof` en procesos largos (API en ejecución).

**Nota sobre fugas de memoria:** `pprof` muestra asignaciones y uso en instantes concretos; una fuga real se confirma comparando perfiles heap en el tiempo (`base` vs `delta` en `go tool pprof`) o monitoreando `inuse_space` bajo carga sostenida. Esta auditoría se basó en ejecuciones cortas de tests/benchmark.

---

## 2. Metodología

1. `go vet ./...`
2. `go test -race -count=1 -timeout=120s ./...`
3. Perfiles por paquete (limitación de `go test`: `-cpuprofile`/`-memprofile` no combinan varios paquetes en un solo archivo):
   - `internal/handler`, `internal/domain/filter`, `internal/domain/datetime`
4. `-mutexprofile` y `-blockprofile` en `internal/handler`
5. Benchmark `BenchmarkBuildConditionSQL_efosAny` en `internal/domain/filter` (800 ms) con `-cpuprofile` y `-memprofile` para obtener gráficos CPU con muestras significativas
6. Gráficos SVG con `go tool pprof -svg` (requiere Graphviz/`dot` en PATH)

---

## 3. Artefactos generados

Directorio: `go_backend/perf_audit/`

| Archivo | Descripción |
|---------|-------------|
| `race_detector.txt` | Salida completa de `go test -race ./...` |
| `go_vet.txt` | Salida de `go vet ./...` |
| `cpu_bench_filter.svg` | Grafo CPU (benchmark `filter`) — **referencia principal CPU** |
| `mem_bench_filter_alloc.svg` | Grafo memoria `alloc_space` (benchmark) |
| `mem_handler_alloc.svg` | Memoria `alloc_space` (tests `handler`, incluye traza si se capturó) |
| `mem_handler_inuse.svg` | Memoria `inuse_space` (tests `handler`) |
| `mem_filter_alloc.svg` | Memoria `alloc_space` (tests `filter`) |
| `cpu_handler.svg`, `cpu_filter.svg`, `cpu_handler_heavy.svg` | CPU de tests unitarios (muestras ~0; tests demasiado breves para el muestreador) |
| `mutex_handler.prof`, `block_handler.prof` | Perfiles binarios (abrir con `go tool pprof`) |
| `*_top.txt` | Salidas texto `go tool pprof -top` |
| `trace_handler.out` | Traza de ejecución (`go tool trace trace_handler.out`) |
| `bench_filter.txt` | Resultado numérico del benchmark |

Para ver un perfil interactivo:

```bash
cd go_backend/perf_audit
go tool pprof -http=:0 cpu_bench_filter.prof
```

---

## 4. Hallazgos relevantes

### 4.1 Concurrencia y bloqueos (código revisado)

Puntos con sincronización explícita:

- `internal/domain/auth/jwt.go`: `sync.RWMutex` en `JWTDecoder` para caché de claves JWKS.
- `internal/domain/event/bus.go`: `sync.Mutex` en publicación/suscripción; `Publish` copia handlers bajo lock y libera el lock antes de ejecutar handlers — orden coherente para evitar deadlocks entre handlers que vuelven a publicar.
- `internal/domain/cfdi/iva.go`: `Mutex` + `WaitGroup` en flujo paralelo localizado (revisar que no haya ciclos de espera si se amplía el uso).

El detector de carreras no reportó accesos concurrentes incorrectos en la suite actual.

### 4.2 CPU (benchmark `filter`)

Top plano (~77% del tiempo de muestra) en `buildConditionSQL` durante `BenchmarkBuildConditionSQL_efosAny` — coherente con un micro-benchmark que ejerce solo esa ruta.

### 4.3 Memoria (tests / benchmark)

En perfiles de tests, aparecen costes de arranque del binario de test (`runtime/pprof`, `compress/flate`, inicialización de dependencias como `inflection`, AWS S3 endpoints, etc.). No implican por sí solos una fuga; sí indican que perfiles muy cortos mezclan **init** y lógica.

---

## 5. Recomendaciones

1. **Producción / staging:** exponer (solo red interna o autenticado) `net/http/pprof` en el servidor HTTP o recoger perfiles bajo carga real; comparar dos heap profiles separados por intervalo (`inuse_space`).
2. **CI:** mantener `go test -race` en paquetes con concurrencia; ampliar benchmarks en rutas críticas (auth, CRUD masivo, parsers CFDI).
3. **Deadlocks:** si aparece un bloqueo en producción, conservar goroutine dump (`SIGQUIT` o endpoint de debug) y una traza (`-trace`) de la reproducción.
4. **Graphviz:** instalado vía Homebrew para esta auditoría (`graphviz` 14.x); sin `dot`, `go tool pprof -svg` falla.

---

## 6. Comandos reproducibles

```bash
cd go_backend
go vet ./...
go test -race -count=1 -timeout=120s ./...

# Benchmark + perfiles (CPU con muestras)
go test -run='^$' -bench=BenchmarkBuildConditionSQL_efosAny -benchtime=800ms \
  -cpuprofile=perf_audit/cpu_bench_filter.prof \
  -memprofile=perf_audit/mem_bench_filter.prof \
  ./internal/domain/filter

# SVG (requiere graphviz)
go tool pprof -svg -output=perf_audit/cpu_bench_filter.svg perf_audit/cpu_bench_filter.prof
go tool pprof -svg -output=perf_audit/mem_bench_filter_alloc.svg -sample_index=alloc_space perf_audit/mem_bench_filter.prof
```

---

## 7. Figuras (vista rápida en el repositorio)

Abrir en el IDE o navegador los SVG bajo `perf_audit/`:

- [CPU benchmark filter](perf_audit/cpu_bench_filter.svg)
- [Memoria alloc benchmark filter](perf_audit/mem_bench_filter_alloc.svg)
- [Memoria alloc tests handler](perf_audit/mem_handler_alloc.svg)
- [Memoria inuse tests handler](perf_audit/mem_handler_inuse.svg)
- [Memoria alloc tests filter](perf_audit/mem_filter_alloc.svg)
