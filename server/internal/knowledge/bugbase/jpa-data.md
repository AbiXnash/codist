# JPA, Hibernate and data access

## N+1 queries {#n-plus-one}
severity: high
category: performance
tags: lazy @OneToMany @ManyToOne getItems loop stream map findAll JOIN FETCH @EntityGraph batch size
see: (Hibernate)

Loading a list of entities and then touching a lazy association for each one runs one query per row. Ten
rows in a test, ten thousand in production. Serialising entities with Jackson triggers the same thing
invisibly.

```java
// bad
for (Order o : orderRepo.findAll()) total += o.getLines().size();
// better
@Query("select o from Order o join fetch o.lines")
List<Order> findAllWithLines();
```

## Repository call inside a loop {#query-in-loop}
severity: high
category: performance
tags: findById save loop for stream repository batch saveAll findAllById IN
see: (data access)

`findById` or `save` per element of a list makes a round trip per element. Fetch with `findAllById`, write
with `saveAll` and a JDBC batch size, or one bulk `UPDATE`.

## LazyInitializationException outside the transaction {#lazy-init-outside-tx}
severity: high
category: bug
tags: LazyInitializationException lazy no session open-in-view DTO transaction controller
see: (Hibernate)

A lazy association accessed after the transaction (in a controller, in Jackson, in an async task) fails,
or with `spring.jpa.open-in-view=true` (the default) it silently runs queries during rendering and holds the
connection for the whole request. Load what the response needs inside a `@Transactional(readOnly = true)`
service method and return DTOs.

## Entity modified without save, or saved needlessly {#dirty-checking}
severity: low
category: design
tags: managed entity dirty checking save setter transaction detached merge
see: (Hibernate)

Inside a transaction, changes to a managed entity are flushed automatically; outside one (detached), setters
change nothing in the database. Calling `save` on a managed entity is harmless but hides which objects are
detached.

## EAGER fetching {#eager-fetch}
severity: medium
category: performance
tags: FetchType.EAGER @ManyToOne @OneToMany join cartesian product
see: (Hibernate)

`EAGER` loads associations on every query, joins collections into cartesian products, and cannot be turned
off per query. `@ManyToOne` is eager by default; prefer `LAZY` and fetch explicitly where needed.

## Unbounded queries {#unbounded-query}
severity: medium
category: performance
tags: findAll List Pageable paging limit stream memory export
see: (data access)

`findAll()` or a query without paging returns every row; it works on test data and exhausts memory in
production. Page, stream with a cursor, or limit.

## Missing unique constraint behind a uniqueness check {#missing-unique-index}
severity: medium
category: bug
tags: unique email username existsBy @Column(unique) index constraint duplicate
see: (data integrity)

Checking "does this email exist?" in code without a unique constraint in the schema allows duplicates under
concurrency. The constraint is the real check; the code check only gives a nicer message.

## JPQL or SQL built by concatenation {#sql-injection}
severity: critical
category: security
tags: createQuery createNativeQuery + concatenation String.format JdbcTemplate query parameter injection
see: Sonar S2077 S3649, SpotBugs SQL_NONCONSTANT_STRING_PASSED_TO_EXECUTE

Concatenating input into SQL or JPQL lets an attacker change the query. Bind parameters, or for dynamic
`ORDER BY`, map the input to an allow-list of columns.

```java
// bad
em.createQuery("select u from User u where u.name = '" + name + "'");
// better
em.createQuery("select u from User u where u.name = :name").setParameter("name", name);
```

## @Modifying query without clearing the context {#modifying-query-stale}
severity: medium
category: bug
tags: @Modifying @Query update delete clearAutomatically flushAutomatically stale entity
see: (Spring Data)

A bulk `@Modifying` update bypasses the persistence context: entities already loaded in the transaction keep
their old values and may be written back over the update. Use `clearAutomatically = true` or reload.

## Lombok @Data on entities {#lombok-data-entity}
severity: medium
category: bug
tags: Lombok @Data @ToString @EqualsAndHashCode entity lazy StackOverflowError
see: (JPA)

`@Data` generates `equals`, `hashCode` and `toString` over every field, including lazy collections and
back-references: logging an entity triggers queries or a `StackOverflowError`, and hash codes change on save.
Use `@Getter`/`@Setter` and write identity-based equality.

## Cascade and orphan removal surprises {#cascade-remove}
severity: medium
category: bug
tags: CascadeType.ALL REMOVE orphanRemoval delete @ManyToOne cascade
see: (JPA)

`CascadeType.ALL` on a `@ManyToOne` deletes the shared parent when one child is deleted. `orphanRemoval`
deletes rows when an element is removed from a collection, including when the collection is replaced.

## Transactions and the connection pool {#long-transaction}
severity: medium
category: performance
tags: @Transactional long running batch loop connection pool HikariCP timeout
see: (data access)

A transaction around a long loop, file processing or user think time holds a pooled connection and row
locks for its whole duration. Process in chunks with a transaction per chunk.
