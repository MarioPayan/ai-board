# Notas del diseño

- Cabecera y agentes **fijos**; el resto hace scroll.
- Los pasos hechos se atenúan, no se borran.

```mermaid
graph LR
  hooks --> agents.jsonl --> board
```
