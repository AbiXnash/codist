# Spring and Spring Boot

## Dependency never injected {#dependency-not-injected}
severity: critical
category: spring
tags: @Service @Component @Repository field private final constructor @Autowired @RequiredArgsConstructor null NullPointerException wiring bean
see: (Spring wiring)

A bean declares a field for another bean (a repository, a client) but nothing sets it: no constructor
parameter, no `@Autowired`, no Lombok `@RequiredArgsConstructor` on a final field. Spring creates the bean
happily with the field left null, the application starts, and the first request that touches the field
fails with a NullPointerException. Unit tests that use `@InjectMocks` set the field by reflection and hide it.

```java
// bad
@Service public class CustomerService {
    private CustomerRepository customers;     // never set
}
// better
@Service public class CustomerService {
    private final CustomerRepository customers;
    public CustomerService(CustomerRepository customers) { this.customers = customers; }
}
```

## Field injection {#field-injection}
severity: low
category: spring
tags: @Autowired @Inject field private constructor injection final testing
see: Sonar S6813 (Spring)

`@Autowired` on fields hides a class's dependencies, prevents `final`, allows a half-built object in tests
(`new Service()` has null fields) and lets circular dependencies grow unnoticed. Constructor injection makes
dependencies explicit and immutable, and Spring fails at startup if one is missing.

## @Transactional on a self-invoked, private or final method {#transactional-proxy-bypass}
severity: high
category: spring
tags: @Transactional this. self-invocation private final static proxy AOP same class
see: Sonar S2229 S2230

Spring applies `@Transactional` (and `@Cacheable`, `@Async`, `@Retryable`, `@PreAuthorize`) through a proxy.
Calling the annotated method from another method of the same class (`this.save()`), or annotating a private,
final or static method, bypasses the proxy: no transaction starts, nothing rolls back, and there is no error.

```java
// bad
public void importAll(List<Row> rows) { rows.forEach(this::importOne); }
@Transactional public void importOne(Row r) { ... }   // never transactional when called above
// better: move importOne to another bean, or put @Transactional on the public entry point
```

## Multi-step write without a transaction {#missing-transaction}
severity: high
category: spring
tags: save delete multiple repositories @Transactional partial write consistency rollback
see: (Spring data)

Two or more writes (save the order, decrement stock, write the payment) without a surrounding
transaction commit one by one. A failure in the middle leaves the database half-updated.

## Checked exceptions do not roll back {#rollback-checked}
severity: medium
category: spring
tags: @Transactional rollbackFor checked exception IOException rollback commit
see: (Spring transactions)

By default Spring rolls back only on `RuntimeException` and `Error`. A checked exception thrown out of a
`@Transactional` method commits the work done so far, unless `rollbackFor` says otherwise.

## Remote calls inside a database transaction {#remote-call-in-transaction}
severity: medium
category: performance
tags: @Transactional RestTemplate WebClient HTTP Kafka email external call connection pool long transaction
see: (Spring transactions)

Calling an HTTP API, sending email or publishing to a broker inside `@Transactional` keeps a database
connection and locks for the duration of the call. Under load the pool is exhausted; if the transaction
rolls back after the call, the email is already sent. Do remote work before or after the transaction (or use
an outbox / `@TransactionalEventListener(phase = AFTER_COMMIT)`).

## readOnly and propagation surprises {#transaction-propagation}
severity: low
category: spring
tags: readOnly REQUIRES_NEW NESTED propagation inner transaction UnexpectedRollbackException
see: (Spring transactions)

An inner `REQUIRED` method that throws marks the shared transaction rollback-only; the outer method catching
the exception then fails at commit with `UnexpectedRollbackException`. `REQUIRES_NEW` opens a second
connection and can deadlock with the outer transaction on the same rows.

## Controller returns entities {#entity-in-api}
severity: medium
category: design
tags: @RestController @GetMapping return entity JPA lazy Jackson DTO serialization password
see: (API design)

Returning JPA entities from controllers couples the API to the schema, serialises lazy relations (extra
queries or `LazyInitializationException`), may expose fields like password hashes, and loops on bidirectional
relations. Map to response DTOs.

## Request body not validated {#unvalidated-request-body}
severity: medium
category: spring
tags: @RequestBody @Valid @Validated @NotNull @NotBlank @Size controller validation input
see: (Spring validation)

Constraint annotations on a request DTO do nothing unless the controller parameter is `@Valid` (or the
class `@Validated` for method parameters). Invalid input then reaches the service and the database.

## Wrong HTTP status for errors {#wrong-http-status}
severity: low
category: design
tags: ResponseEntity status 200 500 404 400 409 @ResponseStatus @ExceptionHandler ControllerAdvice
see: (API design)

Returning 200 with an error body, or letting a "not found" become a 500, breaks clients, retries and
monitoring. Map domain exceptions to statuses in one `@RestControllerAdvice`.

## @Value and configuration mistakes {#config-binding}
severity: medium
category: spring
tags: @Value ${} default property static field @ConfigurationProperties profile environment
see: (Spring configuration)

`@Value` on a static field is never set; a placeholder without a default fails startup in environments that
lack it, or with a default silently runs with test values in production. Typed `@ConfigurationProperties`
with validation catch these at startup.

## Circular dependencies {#circular-dependency}
severity: medium
category: spring
tags: circular dependency @Lazy BeanCurrentlyInCreationException constructor injection design
see: (Spring wiring)

Two services that depend on each other usually share a responsibility that belongs in a third bean. Since
Spring Boot 2.6 the cycle fails at startup; `@Lazy` hides it rather than fixing it.

## Bean scope mismatches {#scope-mismatch}
severity: medium
category: spring
tags: @Scope prototype request session singleton inject ObjectProvider
see: (Spring scopes)

A prototype or request-scoped bean injected into a singleton is created once and shared forever. Use
`ObjectProvider<T>`, a scoped proxy, or pass the value as a parameter.

## Scheduled jobs overlap or run on every instance {#scheduled-pitfalls}
severity: medium
category: spring
tags: @Scheduled fixedRate cron cluster instances ShedLock overlap thread pool single thread
see: (Spring scheduling)

`@Scheduled` runs on every instance of the service (duplicate emails, double billing) and, by default, on a
single thread shared by all jobs: one slow job delays the others. `fixedRate` can start a run before the last
one ended. Use a distributed lock (e.g. ShedLock) for cluster-wide jobs.

## Caching pitfalls {#cache-pitfalls}
severity: medium
category: spring
tags: @Cacheable key eviction @CacheEvict TTL mutable cached object self-invocation null
see: (Spring cache)

`@Cacheable` is bypassed by self-invocation, caches nulls unless told not to, uses all parameters as the key
by default, and returns the same mutable object to every caller. Without eviction or TTL, stale data lives
forever.

## Blocking calls in reactive code {#blocking-in-reactive}
severity: high
category: performance
tags: WebFlux Mono Flux block() blocking JDBC RestTemplate Thread.sleep event loop
see: (Reactor)

`block()`, JDBC, `RestTemplate` or `Thread.sleep` inside a `Mono`/`Flux` pipeline blocks an event-loop
thread; a handful of slow calls stall the whole server.

## HTTP clients without timeouts {#http-client-timeout}
severity: high
category: performance
tags: RestTemplate WebClient HttpClient timeout connect read Feign retries resilience
see: (resilience)

A `RestTemplate` or `HttpClient` built with defaults has no read timeout: when the remote service hangs,
every request thread waits forever and the caller goes down with it. Set connect and read timeouts, and bound
retries.

## Events published inside a transaction {#event-before-commit}
severity: medium
category: spring
tags: ApplicationEventPublisher @EventListener @TransactionalEventListener Kafka publish commit outbox
see: (Spring events)

An `@EventListener` runs immediately, inside the publisher's transaction; a message sent from it announces
data that may still roll back. `@TransactionalEventListener(phase = AFTER_COMMIT)` or an outbox table keeps
events and data consistent.

## Actuator and debug endpoints exposed {#exposed-actuator}
severity: high
category: security
tags: management.endpoints.web.exposure.include * actuator env heapdump debug true h2-console
see: Sonar S4507

Exposing all actuator endpoints publishes environment variables (secrets), heap dumps and thread dumps.
`debug=true`, the H2 console and Spring Boot DevTools must not reach production.
