# Concurrency

## Mutable state in a singleton bean {#mutable-singleton-state}
severity: high
category: concurrency
tags: @Service @Component @Controller singleton field state counter list map request thread-safe
see: Sonar S3749

Spring beans are singletons shared by every request thread. A non-final field that a request writes to
(a "current user", a running total, a list of results) is shared between concurrent requests: one user's
data leaks into another's response, or counts are lost. It passes every single-threaded test.

```java
// bad
@Service class ReportService {
    private List<Line> lines = new ArrayList<>();
    Report build(Query q) { lines.clear(); ... }
}
// better: keep request data in local variables or parameters
@Service class ReportService {
    Report build(Query q) { List<Line> lines = new ArrayList<>(); ... }
}
```

## Check-then-act race {#check-then-act}
severity: high
category: concurrency
tags: exists then save findBy isPresent containsKey put race unique duplicate double booking
see: (classic race condition)

Checking a condition and acting on it in two steps lets two requests both pass the check: two accounts with
the same email, a seat booked twice, a coupon used twice. In the database, only a unique constraint or a lock
makes it atomic; in memory, `putIfAbsent`, `computeIfAbsent` or a lock.

```java
// bad
if (repo.existsByEmail(email)) throw new Conflict();
repo.save(new User(email));
// better: let the database decide, translate the violation
try { repo.save(new User(email)); }
catch (DataIntegrityViolationException e) { throw new Conflict(); }
```

## Lost update (read-modify-write without locking) {#lost-update}
severity: high
category: concurrency
tags: balance stock quantity increment decrement save @Version optimistic locking pessimistic UPDATE
see: (classic race condition)

Loading an entity, changing a counter in Java and saving it lets two concurrent requests overwrite each
other: two purchases decrement stock once. `@Version` (optimistic locking), `SELECT ... FOR UPDATE`, or an
atomic `UPDATE stock SET qty = qty - 1 WHERE qty > 0` prevent it.

## Non-atomic compound operations on shared fields {#non-atomic-compound}
severity: high
category: concurrency
tags: count++ volatile static counter AtomicInteger LongAdder ConcurrentHashMap get put
see: SpotBugs AT_NONATOMIC_OPERATIONS_ON_SHARED_VARIABLE VO_VOLATILE_INCREMENT

`count++` on a shared field, or `map.put(k, map.get(k) + 1)`, is three steps; concurrent threads lose
increments. `volatile` makes the value visible, not the update atomic.

```java
// better
private final LongAdder requests = new LongAdder();
counts.merge(key, 1, Integer::sum);   // on a ConcurrentHashMap
```

## HashMap or ArrayList shared between threads {#unsafe-shared-collection}
severity: high
category: concurrency
tags: HashMap ArrayList static cache shared thread ConcurrentHashMap Collections.synchronizedMap
see: SpotBugs IS2_INCONSISTENT_SYNC

A plain `HashMap` used as a cache by several threads can lose entries, return wrong values, and with older
JDKs loop forever during a resize. Use `ConcurrentHashMap` (and its atomic methods) or a cache library.

## Double-checked locking without volatile {#double-checked-locking}
severity: medium
category: concurrency
tags: singleton lazy init synchronized instance null check volatile
see: Sonar S2168, SpotBugs DC_DOUBLECHECK

Without `volatile`, another thread can see a non-null reference to a partially constructed object. In
Spring, let the container own singletons, or use a holder class.

## Synchronizing on the wrong object {#bad-lock-object}
severity: medium
category: concurrency
tags: synchronized String Boolean Integer parameter getClass lock literal
see: Sonar S2445 S1860, SpotBugs JLM_JSR166_UTILCONCURRENT_MONITORENTER

Locking on a String literal, a boxed value, a method parameter or `getClass()` either shares the lock with
unrelated code or locks nothing at all. Lock on a private final object.

## Blocking call while holding a lock {#blocking-under-lock}
severity: medium
category: concurrency
tags: synchronized Thread.sleep HTTP call database lock contention deadlock
see: SpotBugs SWL_SLEEP_WITH_LOCK_HELD

Sleeping, calling a remote API or querying the database inside `synchronized` serialises all callers behind
the slowest network call; with two locks it deadlocks. With virtual threads, `synchronized` around blocking
I/O also pins the carrier thread.

## Unbounded thread pools and executors {#unbounded-executor}
severity: medium
category: concurrency
tags: Executors.newCachedThreadPool newFixedThreadPool new Thread @Async executor queue shutdown
see: PMD DoNotUseThreads

`newCachedThreadPool` creates a thread per task under load; `newFixedThreadPool` has an unbounded queue
that grows until memory runs out. Executors created per request are never shut down. Use one configured
`ThreadPoolTaskExecutor` with a bounded queue and a rejection policy.

## @Async pitfalls {#async-pitfalls}
severity: medium
category: spring
tags: @Async void exception CompletableFuture self-invocation SecurityContext transaction executor
see: (Spring)

`@Async` only works through the proxy (no self-invocation), runs on the default executor unless configured,
loses exceptions from `void` methods, and does not carry the caller's transaction, `SecurityContext` or MDC.

## CompletableFuture without an executor or error handling {#completablefuture-pitfalls}
severity: medium
category: concurrency
tags: CompletableFuture supplyAsync runAsync ForkJoinPool commonPool join get exceptionally
see: (Java concurrency)

`supplyAsync` without an executor runs blocking I/O on the shared `ForkJoinPool.commonPool`, starving parallel
streams and other async work. An exception is only seen by whoever calls `join`/`get`; fire-and-forget futures
lose errors silently.

## ThreadLocal not cleared on pooled threads {#threadlocal-leak}
severity: medium
category: concurrency
tags: ThreadLocal remove MDC request context filter pooled thread leak
see: (Java concurrency)

Request threads are reused. A `ThreadLocal` (or MDC entry) set during one request and not removed in a
`finally` is visible to the next request on that thread: wrong tenant, wrong user in logs, memory leaks.

## wait/notify misuse {#wait-notify}
severity: medium
category: concurrency
tags: wait notify notifyAll while condition spurious wakeup
see: Sonar S2273 S2274

`wait()` must be called in a loop that re-checks the condition (spurious wakeups) while holding the lock.
Prefer `java.util.concurrent` (`CountDownLatch`, `BlockingQueue`, `CompletableFuture`).

## Calling Thread.run instead of start {#thread-run}
severity: high
category: concurrency
tags: Thread run start Runnable
see: Sonar S1217

`thread.run()` executes the task on the current thread; nothing runs in parallel and the caller blocks.
