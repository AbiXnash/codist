# Dead code and leftovers

## Commented-out code {#commented-out-code}
severity: low
category: design
tags: comment commented out method if else block disabled // old code
see: Sonar S125

Code left in comments goes stale at once: nobody compiles it, nobody knows whether it is needed, and git
already keeps the old version. When a check or a branch was commented out to get past a failure, the failure is
still there and the protection is gone, which is worth more attention than the clutter.

```java
// bad
// if (order.getTotal() == null) {
//     throw new IllegalArgumentException("no total");
// }
ship(order);
```

## A comment admits a bug {#bug-comment}
severity: medium
category: bug
tags: FIXME TODO HACK XXX BUG workaround comment known issue temporary
see: Sonar S1134 S1135

"FIXME", "HACK", "this is a bug" and "temporary" mark a known defect that was shipped. They have no owner or
date, so they outlive the people who wrote them.

## Unused variables, fields, methods and imports {#unused-code}
severity: low
category: bug
tags: unused variable private field method import never read dead store
see: Sonar S1481 S1068 S1144 S1128, SpotBugs DLS_DEAD_LOCAL_STORE URF_UNREAD_FIELD

A value computed and never used is often a real bug: the result was meant to go somewhere, such as a validated
copy that the next line ignores in favour of the original. When it really is unused it is only noise.

```java
// bad: the trimmed value is thrown away
String clean = input.trim();
repository.save(input);
```

## Conditions that decide nothing {#dead-condition}
severity: medium
category: bug
tags: if true false always constant condition identical branches same else empty block unreachable return
see: Sonar S2583 S1871 S3923 S108 S1763

`if (true)`, a value compared with itself, both branches doing the same thing, an empty block, or code after a
return all mean the author intended something the code does not do. They usually come from debugging or a bad merge.

## Method cost: work per item {#method-cost}
severity: medium
category: performance
tags: complexity nested loops O(n2) contains list stream cost round trips batch
see: Sonar S3776

What a method costs is set by its loops and what they call. A database or HTTP call inside a loop costs a round
trip per item; `List.contains` inside a loop makes it quadratic. Index one side into a Map or Set, and fetch in bulk.

```java
// bad: O(n x m)
for (Order o : orders) if (blocked.contains(o.getCustomerId())) reject(o);
// better: O(n + m)
Set<Long> blockedIds = new HashSet<>(blocked);
for (Order o : orders) if (blockedIds.contains(o.getCustomerId())) reject(o);
```

## Logs that cannot diagnose anything {#useless-log}
severity: low
category: logging
tags: log error message exception stack trace id context placeholder level
see: Sonar S2629, PMD GuardLogStatement

A useful log line names what failed, for which record, and carries the exception. "error" or "failed" helps nobody
at 3 am; a catch that logs only a message loses the stack trace.

```java
// bad
catch (GatewayException e) { log.error("failed"); }
// better
catch (GatewayException e) { log.error("charging order {} failed", orderId, e); }
```
