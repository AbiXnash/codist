# Performance, logging and operations

## Remote call per item in a loop {#remote-call-in-loop}
severity: high
category: performance
tags: loop for stream RestTemplate WebClient HTTP call per item batch bulk reconciliation payment
see: (performance)

Calling an API once per element (one payment status per transaction in a reconciliation, one price per
product) multiplies latency by the list size and hammers the remote service. Use a bulk endpoint, call in
parallel with a bounded pool, or cache.

## Logging inside hot loops {#logging-in-loop}
severity: low
category: performance
tags: log.info loop for each item debug verbose log volume
see: (performance)

A log line per element of a large list floods log storage and slows the loop. Log a summary (counts, first
failures) after the loop; keep per-item detail at debug, or only for failures.

## Expensive work in log arguments {#log-argument-cost}
severity: low
category: performance
tags: log.debug string concatenation toString isDebugEnabled placeholder {}
see: Sonar S2629, PMD GuardLogStatement

`log.debug("x=" + bigObject)` builds the string even when debug is off. Use placeholders
(`log.debug("x={}", x)`) and guard truly expensive computations.

## System.out or printStackTrace instead of a logger {#console-output}
severity: low
category: logging
tags: System.out.println System.err printStackTrace logger SLF4J
see: Sonar S106 S1148

Console output has no level, no timestamp, no request id, and is often not collected. Use the logger.

## Same message logged twice, or log-and-throw {#duplicate-logging}
severity: low
category: logging
tags: log error throw rethrow duplicate log twice same message
see: (logging)

Logging an exception and then rethrowing it logs it again at every layer: one failure shows up as five errors.
Either handle it (and log) or rethrow (and let the top layer log once).

## Wrong log level {#log-level}
severity: low
category: logging
tags: log.error expected 404 validation info warn debug alert noise
see: (logging)

Logging expected conditions (validation failures, 404s) at error level pages people for nothing and hides
real errors; logging real failures at debug hides them.

## Creating expensive objects per call {#expensive-object-per-call}
severity: medium
category: performance
tags: new ObjectMapper RestTemplate HttpClient Gson DocumentBuilder per request
see: (performance)

`ObjectMapper`, HTTP clients and XML factories are expensive to build and thread-safe once configured.
Create them once (as beans) instead of per call.

## Caches without bounds {#unbounded-cache}
severity: medium
category: performance
tags: HashMap cache static map memory leak eviction Caffeine maximumSize TTL
see: (performance)

A `static Map` used as a cache grows forever: a slow memory leak that ends in `OutOfMemoryError` after days.
Use a bounded cache with expiry.

## Retrying without backoff or idempotency {#unsafe-retry}
severity: medium
category: bug
tags: retry loop @Retryable backoff idempotent payment charge duplicate
see: (resilience)

Retrying immediately in a tight loop amplifies an outage; retrying a non-idempotent operation (a payment, an
email) duplicates it. Retry with backoff and only idempotent calls, or use an idempotency key.

## Streams misuse {#stream-misuse}
severity: low
category: performance
tags: parallelStream stream collect count sorted findFirst forEach side effects
see: (Java streams)

`parallelStream()` on small lists or with blocking calls runs on the shared common pool and is slower;
side effects inside `map`/`forEach` with parallel streams race. `sorted().findFirst()` sorts everything to
get a minimum (`min` does not).

## String formatting mistakes {#format-string}
severity: low
category: bug
tags: String.format %s %d placeholder count arguments log {} MessageFormat
see: Sonar S2275 S3457

A format string with fewer placeholders than arguments drops values; a wrong conversion (`%d` for a String)
throws at run time, often only on the error path where nobody tested it.
