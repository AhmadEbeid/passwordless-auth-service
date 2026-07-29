# Open decisions (deferred — with triggers)

These decisions are **consciously deferred, not forgotten**. Each is cheap to
decide but wrong to build ahead of a real consumer. Each row names the trigger that
turns it into an ADR + implementation. When a trigger fires, write the ADR
*then* build the mechanism.

| Decision | Deferred until… |
|---|---|
| **Error-taxonomy specifics** | a feature needs an error category the thin `platform/errors` taxonomy + HTTP mapper does not already cover |
| **Feature-flag implementation** | deploy must be decoupled from release for real users |
| **Audit-store implementation** | the first sensitive-resource feature exists |
| **Rate limiting** | a publicly reachable abuse surface exists |
| **Idempotency keys** | a non-idempotent public POST is retried by clients |
| **Caching** | a read path is *measured* hot |
| **sqlc / data-access codegen** | hand-writing repos against `db.DBTX` becomes painful — auth currently hand-writes its `postgres/` adapter; revisit when the boilerplate or query volume justifies generation |
| **Outbox worker** | the first genuine cross-feature async need appears (ADR-0002) |
| **SLOs / alerting** | running in production with real users |
| **Multi-tenancy** | more than one tenant must be isolated in one deployment |

`flags/` and `audit/` are deliberately **not** pre-built platform packages: they
are domain-shaped and must be defined by their first consumer, as a
consumer-owned port, not a speculative central service.
