# Java language and core library

## Null dereference on some path {#null-dereference}
severity: high
category: bug
tags: null NullPointerException NPE dereference return null map get find optional
see: Sonar S2259, SpotBugs NP_NULL_ON_SOME_PATH

A value that can be null on at least one path is used without a check: a `Map.get` miss, a method that
returns null "when not found", a field set only in some constructors, a JSON property that may be absent.
The NPE fires far from the cause, usually only for the rare input that takes that path, so tests pass.

```java
// bad
Customer c = cache.get(id);
return c.getName();
// better
Customer c = cache.get(id);
if (c == null) throw new NotFoundException("customer " + id);
return c.getName();
```

## Optional.get() without a presence check {#optional-get-unchecked}
severity: medium
category: bug
tags: Optional get isPresent orElseThrow NoSuchElementException findById
see: Sonar S3655

`Optional.get()` throws `NoSuchElementException` when empty, which turns "not found" into a 500 with an
unhelpful message. The intent of Optional is lost.

```java
// bad
return repo.findById(id).get();
// better
return repo.findById(id).orElseThrow(() -> new NotFoundException("order " + id));
```

## Comparing objects or strings with == {#reference-equality}
severity: high
category: bug
tags: == equals String Integer Long boxed compare identity
see: Sonar S4973 S1698, SpotBugs ES_COMPARING_STRINGS_WITH_EQ RC_REF_COMPARISON

`==` compares identity. It "works" for interned string literals and for boxed numbers between -128 and
127 (the Integer cache), then fails for real data: user input, values from the database, ids above 127.

```java
// bad
if (order.getStatus() == "PAID") ...
if (a.getId() == b.getId()) ...      // Long ids
// better
if ("PAID".equals(order.getStatus())) ...
if (Objects.equals(a.getId(), b.getId())) ...
```

## equals without hashCode, or mutable fields in hashCode {#equals-hashcode-contract}
severity: high
category: bug
tags: equals hashCode HashSet HashMap contract entity mutable Lombok @Data @EqualsAndHashCode
see: Sonar S1206, SpotBugs HE_EQUALS_USE_HASHCODE

Objects that are equal must have the same hash code, otherwise `HashSet.contains` and `HashMap.get`
miss them. Hashing on fields that change (a generated id assigned on save, a status) moves the object to
another bucket while it sits in a set. On JPA entities, Lombok `@Data` builds both from every field,
including lazy collections.

```java
// better for an entity: identity by id, stable hash
@Override public boolean equals(Object o) {
    return this == o || (o instanceof Order other && id != null && id.equals(other.id));
}
@Override public int hashCode() { return getClass().hashCode(); }
```

## BigDecimal from a double, or compared with equals {#bigdecimal-precision}
severity: high
category: bug
tags: BigDecimal double new BigDecimal valueOf equals compareTo scale money price amount
see: Sonar S2111, SpotBugs DMI_BIGDECIMAL_CONSTRUCTED_FROM_DOUBLE

`new BigDecimal(0.1)` is 0.1000000000000000055511151231257827…, so money sums drift. `equals` also
compares scale: `2.0` and `2.00` are not equal, while `compareTo` says they are. Money in `double` loses
cents.

```java
// bad
BigDecimal price = new BigDecimal(19.99);
if (total.equals(expected)) ...
// better
BigDecimal price = new BigDecimal("19.99");   // or BigDecimal.valueOf(19.99)
if (total.compareTo(expected) == 0) ...
```

## Integer overflow before widening {#int-overflow}
severity: medium
category: bug
tags: int long overflow multiply millis seconds cast Math.multiplyExact timeout size
see: Sonar S2184, SpotBugs ICAST_INTEGER_MULTIPLY_CAST_TO_LONG

`int` arithmetic overflows silently and is widened only after: `long ms = days * 24 * 60 * 60 * 1000`
overflows at 25 days. Sizes, offsets and money in cents hit the same wall.

```java
// bad
long ms = days * 24 * 60 * 60 * 1000;
// better
long ms = days * 24L * 60 * 60 * 1000;     // or Duration.ofDays(days).toMillis()
```

## Ignored return value of an immutable operation {#ignored-return-value}
severity: medium
category: bug
tags: return value ignored String trim replace toUpperCase BigDecimal add plus LocalDate immutable
see: Sonar S2201, SpotBugs RV_RETURN_VALUE_IGNORED

Strings, `BigDecimal`, `java.time` types and many builders are immutable: `s.trim()` or `total.add(x)`
on its own line does nothing. `File.delete()`, `mkdirs()` and `Lock.tryLock()` report failure only through
their return value.

```java
// bad
name.trim();
total.add(item.getPrice());
// better
name = name.trim();
total = total.add(item.getPrice());
```

## Resource not closed on every path {#resource-leak}
severity: high
category: bug
tags: close InputStream OutputStream Reader Connection Statement ResultSet try-with-resources Files.lines stream leak
see: Sonar S2095, SpotBugs OBL_UNSATISFIED_OBLIGATION OS_OPEN_STREAM, PMD CloseResource

Streams, JDBC connections, statements and `Files.lines` hold file handles or pooled connections. Closing
them at the end of the happy path leaks them when anything throws; the pool runs dry under load and the
service hangs.

```java
// bad
BufferedReader r = Files.newBufferedReader(path);
String first = r.readLine();
r.close();
// better
try (BufferedReader r = Files.newBufferedReader(path)) {
    return r.readLine();
}
```

## Platform default charset or locale {#default-charset-locale}
severity: medium
category: bug
tags: getBytes new String FileReader toUpperCase toLowerCase Locale charset UTF-8 Turkish
see: SpotBugs DM_DEFAULT_ENCODING, PMD UseLocaleWithCaseConversions

`getBytes()`, `new String(bytes)`, `FileReader` and `toUpperCase()` use the machine's defaults. The same
code behaves differently on a server set to another locale: the Turkish dotless i breaks `"title".toUpperCase()`
comparisons, and non-UTF-8 defaults corrupt text.

```java
// better
byte[] b = s.getBytes(StandardCharsets.UTF_8);
String key = code.toUpperCase(Locale.ROOT);
```

## Shared SimpleDateFormat or legacy date types {#legacy-date-api}
severity: medium
category: bug
tags: SimpleDateFormat Date Calendar static thread-safe DateTimeFormatter java.time LocalDate ZoneId timezone
see: Sonar S2885, SpotBugs STCAL_STATIC_SIMPLE_DATE_FORMAT_INSTANCE

`SimpleDateFormat` and `Calendar` keep state while formatting, so one shared instance corrupts dates under
concurrent requests. `Date` has no time zone and month numbers start at 0. `LocalDateTime.now()` silently
uses the server's zone.

```java
// better
private static final DateTimeFormatter DAY = DateTimeFormatter.ISO_LOCAL_DATE;
LocalDate today = LocalDate.now(clock);   // inject a Clock; store Instants in UTC
```

## Swallowed exception {#swallowed-exception}
severity: high
category: bug
tags: catch empty ignore printStackTrace Exception swallow log rethrow
see: Sonar S1166 S108, PMD EmptyCatchBlock

An empty catch, or one that only prints the stack trace, makes the operation look successful. The caller
continues with missing data and the original cause is lost from the logs.

```java
// bad
try { gateway.charge(order); } catch (Exception e) { }
// better
try { gateway.charge(order); }
catch (GatewayException e) { throw new PaymentFailedException(order.getId(), e); }
```

## Catching Exception, Throwable or NullPointerException {#overbroad-catch}
severity: medium
category: bug
tags: catch Exception Throwable Error NullPointerException RuntimeException broad
see: Sonar S1181 S1696, PMD AvoidCatchingNPE AvoidCatchingThrowable

Broad catches also catch programming errors (NPE, IllegalState), `OutOfMemoryError` and `InterruptedException`
wrappers, and treat them as an expected business failure. Catching NPE hides the bug instead of fixing it.

## Exception cause dropped when wrapping {#lost-exception-cause}
severity: medium
category: bug
tags: throw new RuntimeException e.getMessage cause wrap stack trace
see: Sonar S1166, PMD PreserveStackTrace

`throw new ServiceException("failed: " + e.getMessage())` throws away the original stack trace and type, so
the log shows where it was rethrown, not where it broke.

```java
// better
throw new ServiceException("charging order " + id + " failed", e);
```

## InterruptedException swallowed {#interrupted-swallowed}
severity: high
category: concurrency
tags: InterruptedException Thread.sleep wait join interrupt catch
see: Sonar S2142

Catching `InterruptedException` and carrying on clears the thread's interrupt flag: executors cannot shut
down, request timeouts do not stop work, and the application hangs on shutdown.

```java
// better
catch (InterruptedException e) {
    Thread.currentThread().interrupt();
    throw new IllegalStateException("interrupted while waiting for " + what, e);
}
```

## Modifying a collection while iterating it {#concurrent-modification}
severity: high
category: bug
tags: ConcurrentModificationException remove for-each iterator list loop removeIf
see: (Java collections)

Removing from a `List` inside a for-each loop throws `ConcurrentModificationException`, or skips elements
for index loops. It often passes tests with one element and fails with two.

```java
// better
items.removeIf(i -> i.getQuantity() == 0);
```

## Returning null instead of an empty collection {#null-collection}
severity: low
category: design
tags: return null List Collections.emptyList empty Set Map
see: PMD ReturnEmptyCollectionRatherThanNull

Callers of a method returning a collection iterate it without checking; a `null` for "no results" turns an
empty result into an NPE in another class.

## Exposing internal mutable state {#exposed-mutable-state}
severity: medium
category: design
tags: getter return list array mutable defensive copy unmodifiable List.copyOf
see: SpotBugs EI_EXPOSE_REP EI_EXPOSE_REP2

Returning or storing a caller's `List`, array or `Date` directly lets other code change the object's state
behind its back, bypassing validation.

```java
// better
public List<Line> getLines() { return List.copyOf(lines); }
```

## Mutable public static fields {#mutable-static}
severity: medium
category: design
tags: public static List Map array final mutable constant
see: SpotBugs MS_MUTABLE_COLLECTION MS_PKGPROTECT

`public static final List<String> CODES = new ArrayList<>()` is final, but its content can be changed by any
class. Constants that are collections or arrays need to be immutable.

## String concatenation in a loop {#string-concat-loop}
severity: low
category: performance
tags: String += loop StringBuilder concatenation join
see: Sonar S1643, PMD UseStringBufferForStringAppends

Each `+=` on a String copies the whole string; building a CSV or report this way is quadratic.

```java
// better
String csv = rows.stream().map(Row::toCsv).collect(Collectors.joining("\n"));
```

## Regex compiled on every call {#regex-recompiled}
severity: low
category: performance
tags: Pattern.compile String.matches replaceAll split regex loop
see: Sonar S4248

`String.matches`, `replaceAll` and `Pattern.compile` inside a method or loop compile the regex every time.
On hot paths this dominates CPU.

```java
private static final Pattern EMAIL = Pattern.compile("^[^@\\s]+@[^@\\s]+$");
```

## Map iterated by keySet then get {#keyset-get}
severity: low
category: performance
tags: keySet get entrySet Map iteration
see: Sonar S2864, SpotBugs WMI_WRONG_MAP_ITERATOR

Looping over `keySet()` and calling `get(key)` does two lookups per entry; `entrySet()` gives both.

## Identical branches or expressions {#copy-paste-expression}
severity: high
category: bug
tags: same condition both branches identical a == a copy paste if else ternary
see: Sonar S1764 S1871 S3923

`if (a.getStart() > a.getStart())`, both sides of `&&` the same, or `if/else` branches with the same body
are almost always a copy-paste slip where one side should refer to another variable.

## Self-assignment in a constructor or setter {#self-assignment}
severity: high
category: bug
tags: this.name = name parameter shadow field assignment setter constructor
see: Sonar S1656

`name = name;` when the parameter shadows the field assigns the parameter to itself and leaves the field
null; the IDE does not complain.

## Switch without break, or missing default {#switch-fallthrough}
severity: medium
category: bug
tags: switch case break fall through default enum
see: Sonar S128 S131

A missing `break` runs the next case too. A switch over an enum without `default` silently does nothing for a
newly added constant. Switch expressions (`case X ->`) avoid both.

## Floating point used for money or equality {#float-money}
severity: high
category: bug
tags: double float money price amount == equality rounding cents
see: Sonar S1244

`0.1 + 0.2 != 0.3` in binary floating point. Prices and balances in `double` round wrongly and comparisons
with `==` fail. Use `BigDecimal` with an explicit scale and `RoundingMode`, or integer cents.

## Misused Random or weak randomness {#weak-random}
severity: medium
category: security
tags: Random Math.random token password reset id SecureRandom predictable
see: Sonar S2245

`java.util.Random` is predictable. Tokens, reset codes, session ids and anything an attacker could guess
need `SecureRandom` (or `UUID.randomUUID()` for ids).

## Serializable class with a non-serializable field {#serialization-field}
severity: low
category: bug
tags: Serializable session cache field transient serialVersionUID
see: SpotBugs SE_BAD_FIELD

Objects stored in an HTTP session, a distributed cache or a message must be serializable all the way down;
a non-serializable field fails only when the object is actually serialized (replication, restart).

## Wrong value or name copied from elsewhere {#copy-paste-wrong-value}
severity: medium
category: bug
tags: constant prefix key label message log copy paste domain wrong class name
see: (Codist)

A constant, log prefix, cache key, message or metric name copied from another class still names the old
domain: a `"CUSTOMER"` prefix in `ProductService`, an `ORDER_CACHE` key used for invoices. Output is
attributed to the wrong area and dashboards, alerts or cache entries collide.

## Literal duplicating an existing constant {#literal-duplicates-constant}
severity: low
category: design
tags: literal string constant duplicate magic value static final
see: Sonar S1192

A string literal with the same value as a constant in scope drifts when the constant changes: one place gets
the new value, the copy keeps the old one.
