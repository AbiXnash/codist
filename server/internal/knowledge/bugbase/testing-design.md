# Tests and design

## Test without a meaningful assertion {#test-no-assertion}
severity: medium
category: testing
tags: @Test assert assertThat verify no assertion expected
see: Sonar S2699

A test that calls code but asserts nothing passes as long as nothing throws, and keeps passing after the
behaviour breaks.

## @InjectMocks hiding wiring bugs {#injectmocks-hides-wiring}
severity: medium
category: testing
tags: @InjectMocks @Mock Mockito field injection reflection constructor
see: (testing)

`@InjectMocks` sets fields by reflection, so a service whose dependency is never injected in production
still works in the test. Construct the class under test through its constructor, or wire it with Spring in
an integration test.

## Thread.sleep in tests {#sleep-in-tests}
severity: low
category: testing
tags: Thread.sleep test flaky async Awaitility timing
see: Sonar S2925

Sleeping a fixed time makes tests slow when it is too long and flaky when it is too short. Wait for the
condition (Awaitility) or make the clock injectable.

## Tests depending on time, order or the network {#flaky-test}
severity: low
category: testing
tags: LocalDate.now System.currentTimeMillis test order static state real HTTP random port
see: (testing)

Tests that read the real clock, depend on running order through static state, or call real external services
fail on some days, machines or runs.

## Asserting the defect instead of the behaviour {#test-asserts-bug}
severity: medium
category: testing
tags: assertThrows NullPointerException expected exception regression test
see: (testing)

A test asserting that an NPE is thrown locks the bug in: it passes while the bug exists and fails when
someone fixes it. Assert the correct outcome.

## Log-only helper methods {#log-helper-method}
severity: low
category: design
tags: private void log helper start end method println only
see: (design)

A private method whose whole body is one log statement (`endLog()`) adds indirection and hides where the
message comes from; inline the log call where it belongs.

## God classes and long methods {#god-class}
severity: low
category: design
tags: long method many responsibilities service 1000 lines complexity
see: Sonar S138 S1200 S3776

A method that loads, validates, calculates, persists and notifies is hard to test and change safely. Split by
responsibility when a change touches it anyway.

## Magic numbers and duplicated literals {#magic-values}
severity: low
category: design
tags: magic number literal timeout 3600 1000 status code constant
see: Sonar S109 S1192

Unnamed numbers and repeated literals hide meaning and drift apart when one copy changes.

## Unclear commit messages {#commit-message}
severity: info
category: design
tags: commit message fix chore wip update changes
see: (Conventional Commits)

"updated", "fix", "wip" tell reviewers and future readers nothing. A message says what changed and why.

## Deprecated or end-of-life APIs {#deprecated-api}
severity: low
category: design
tags: @Deprecated WebSecurityConfigurerAdapter javax jakarta RestTemplate Date new Integer finalize
see: Sonar S1874 S5738

Deprecated APIs are removed in later versions (`WebSecurityConfigurerAdapter` in Spring Security 6, `javax.*`
in Spring Boot 3). Using them in new code adds upgrade work and often misses security fixes.
