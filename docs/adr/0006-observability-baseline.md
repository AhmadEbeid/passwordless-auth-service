# ADR-0006: Observability baseline

**Status:** Accepted
**Date:** 2026-07-18

## Context

Structured, correlatable telemetry must be present from the first feature —
retrofitting request correlation and structured fields across a live codebase is
expensive. But no trace exporter or backend is chosen yet, and building one
ahead of a consumer is a cost with no payer.

## Decision

- **Structured logging via `slog`.** No ad-hoc `stdout`/`fmt` logging. A logger
  is carried in `context.Context` and code logs through it;
  `platform/observability` provides the setup and context helpers.
- **Request ID** is generated in middleware, returned in a response header, and
  carried in `context.Context` **alongside the logger**, so every log line for a
  request correlates.
- An **OpenTelemetry tracer-provider seam** is wired now — a provider the code
  can create spans against — but **no exporter is wired** until a real need (a
  backend to send traces to) appears.
- **Per-request statement counting.** A `pgx` tracer tallies every statement
  against a counter the HTTP middleware puts on the request context. A request
  crossing `QueryCountThreshold` is logged at warn level, and tests assert an
  upper bound on hot endpoints. This exists because an N+1 is invisible by
  latency alone on a development dataset — one query per row and one query for
  all rows look the same at ten rows — so without a count the regression only
  shows up in production. The tracer records no SQL and no arguments, so it
  cannot leak a phone number or a token hash into a log.

## Consequences

- Every feature emits structured, request-correlated logs from day one.
- An access pattern that degrades into per-row queries is caught by a test or a
  warn line rather than by a latency graph after release.
- Turning on real tracing later is wiring an exporter into an existing seam, not
  instrumenting the codebase after the fact.
- Log sink, trace exporter, and SLOs/alerting are deferred (see
  `open-decisions.md`).
