#!/usr/bin/env python3
"""Plays a team of developers building a multi-module Spring Boot project, so Codist can be watched live.

  python3 eval/sample/simulate_team.py ~/dev/orbit-commerce --pace 20
  python3 eval/sample/simulate_team.py /tmp/try --pace 0 --check     no waiting, then compile the result

The code comes from templates, so it is realistic in shape rather than hand-written. Everything happens in
real Git: several authors, feature branches, pull-request style merges, real conflicts left open for a while,
a clean merge that breaks the build, reverts, squash, rebase, force-push, cherry-pick and tags. Pull requests
are merge commits with a pull request message, and a bare repository beside the project acts as the remote.
Known bugs are planted as it goes and listed in <project>-notes/seeded-bugs.jsonl, which isn't in the project,
so reviews can be checked against them.
"""
import argparse, json, os, random, re, shutil, subprocess, sys, time

DEVS = [("Priya Nair", "priya@orbit.example"), ("Ken Tanaka", "ken@orbit.example"), ("Sara Lindqvist", "sara@orbit.example"),
        ("Marco Rossi", "marco@orbit.example"), ("Aisha Khan", "aisha@orbit.example"), ("Tom Becker", "tom@orbit.example")]

SERVICES = {
    "catalog": {"Product": ["sku", "brand"], "Category": ["slug", "parentCode"], "Brand": ["website", "country"], "PriceList": ["currency", "region"],
                "Attribute": ["unit", "dataType"], "Supplier": ["contactEmail", "taxNumber"], "Promotion": ["couponCode", "channel"], "Review": ["author", "headline"], "Collection": ["season", "theme"]},
    "inventory": {"Warehouse": ["city", "postalCode"], "StockItem": ["sku", "binLocation"], "Reservation": ["orderCode", "holdReason"], "Transfer": ["fromSite", "toSite"],
                  "Adjustment": ["reason", "approvedBy"], "Shipment": ["carrier", "trackingCode"], "Replenishment": ["supplierCode", "leadTime"], "CycleCount": ["zone", "countedBy"]},
    "order": {"Order": ["customerCode", "channel"], "OrderLine": ["orderCode", "sku"], "Cart": ["customerCode", "couponCode"], "Quote": ["customerCode", "validUntil"],
              "ReturnRequest": ["orderCode", "reason"], "Invoice": ["orderCode", "taxNumber"], "Fulfilment": ["orderCode", "warehouse"], "Subscription": ["customerCode", "plan"]},
    "payment": {"Payment": ["orderCode", "method"], "Refund": ["paymentCode", "reason"], "Ledger": ["account", "currency"], "Payout": ["merchant", "iban"],
                "Chargeback": ["paymentCode", "network"], "TaxRule": ["region", "category"], "Wallet": ["customerCode", "currency"], "Settlement": ["batchCode", "bank"]},
    "customer": {"Customer": ["email", "phone"], "Address": ["customerCode", "city"], "Segment": ["rule", "owner"], "Consent": ["customerCode", "purpose"],
                 "SupportTicket": ["customerCode", "subject"], "LoyaltyAccount": ["customerCode", "tier"], "Preference": ["customerCode", "channel"]},
    "shipping": {"Carrier": ["website", "serviceLevel"], "Route": ["origin", "destination"], "Delivery": ["orderCode", "driver"], "Rate": ["zone", "serviceLevel"],
                 "PickupPoint": ["city", "openingHours"], "Manifest": ["carrierCode", "depot"], "Vehicle": ["plate", "depot"]},
    "notification": {"Template": ["locale", "subject"], "Message": ["recipient", "channel"], "Campaign": ["audience", "owner"], "Subscriber": ["email", "locale"],
                     "DeliveryReport": ["messageCode", "provider"], "Webhook": ["url", "eventType"], "Digest": ["recipient", "frequency"]},
}

# Which service calls which, for the Feign clients.
CALLS = {"order": [("catalog", "Product"), ("inventory", "StockItem"), ("customer", "Customer"), ("payment", "Payment")],
         "shipping": [("order", "Order"), ("inventory", "Warehouse")], "payment": [("order", "Order"), ("customer", "Customer")],
         "notification": [("customer", "Customer"), ("order", "Order")], "inventory": [("catalog", "Product")]}


def lower(s):
    return s[0].lower() + s[1:]


def plural(s):
    if s.endswith("y") and s[-2] not in "aeiou":
        return s[:-1] + "ies"
    return s + ("es" if s.endswith(("s", "x", "ch")) else "s")


def kebab(s):
    return re.sub(r"(?<!^)([A-Z])", r"-\1", s).lower()


def cap(s):
    return s[0].upper() + s[1:]


def java_path(service, area, name, test=False):
    kind = "test" if test else "main"
    return f"{service}-service/src/{kind}/java/com/orbit/{service}/{area}/{name}.java"


# ---------------------------------------------------------------- templates

def entity_java(service, e, extras):
    area = lower(e).lower()
    table = kebab(e).replace("-", "_")
    fields = "".join(f"\n    @Column(length = 120)\n    private String {x};\n" for x in extras)
    access = "".join(f"""
    public String get{cap(x)}() {{
        return {x};
    }}

    public void set{cap(x)}(String {x}) {{
        this.{x} = {x};
    }}
""" for x in extras)
    return f"""package com.orbit.{service}.{area};

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.GeneratedValue;
import jakarta.persistence.GenerationType;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;
import jakarta.persistence.Version;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.Objects;

/** A {e} as stored by the {service} service. */
@Entity
@Table(name = "{table}")
public class {e} {{

    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false, unique = true, length = 40)
    private String code;

    @Column(nullable = false, length = 160)
    private String name;

    @Column(nullable = false, length = 20)
    private String status = "ACTIVE";

    @Column(nullable = false)
    private int priority;

    @Column(precision = 14, scale = 2)
    private BigDecimal amount = BigDecimal.ZERO;
{fields}
    @Version
    private long version;

    private Instant createdAt;

    private Instant updatedAt;

    protected {e}() {{
        // for JPA
    }}

    public {e}(String code, String name) {{
        this.code = code;
        this.name = name;
    }}

    @PrePersist
    void onCreate() {{
        createdAt = Instant.now();
        updatedAt = createdAt;
    }}

    @PreUpdate
    void onUpdate() {{
        updatedAt = Instant.now();
    }}

    public boolean isActive() {{
        return "ACTIVE".equals(status);
    }}

    public void archive() {{
        status = "ARCHIVED";
    }}

    public Long getId() {{
        return id;
    }}

    public String getCode() {{
        return code;
    }}

    public String getName() {{
        return name;
    }}

    public void setName(String name) {{
        this.name = name;
    }}

    public String getStatus() {{
        return status;
    }}

    public int getPriority() {{
        return priority;
    }}

    public void setPriority(int priority) {{
        this.priority = priority;
    }}

    public BigDecimal getAmount() {{
        return amount;
    }}

    public void setAmount(BigDecimal amount) {{
        this.amount = amount;
    }}
{access}
    public Instant getCreatedAt() {{
        return createdAt;
    }}

    public Instant getUpdatedAt() {{
        return updatedAt;
    }}

    @Override
    public boolean equals(Object other) {{
        if (this == other) {{
            return true;
        }}
        if (!(other instanceof {e} that)) {{
            return false;
        }}
        return Objects.equals(code, that.code);
    }}

    @Override
    public int hashCode() {{
        return Objects.hash(code);
    }}

    @Override
    public String toString() {{
        return "{e}[" + code + ", " + status + "]";
    }}
}}
"""


def repository_java(service, e):
    area = e.lower()
    return f"""package com.orbit.{service}.{area};

import java.util.List;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface {e}Repository extends JpaRepository<{e}, Long> {{

    Optional<{e}> findByCode(String code);

    boolean existsByCode(String code);

    List<{e}> findByStatus(String status);

    long countByStatus(String status);

    @Query("select x from {e} x where lower(x.name) like lower(concat('%', :text, '%')) order by x.priority desc")
    List<{e}> search(@Param("text") String text);
}}
"""


def dtos_java(service, e, extras):
    area = e.lower()
    req = ",\n".join(f"        String {x}" for x in extras)
    set_args = "".join(f",\n                item.get{cap(x)}()" for x in extras)
    res = ",\n".join(f"        String {x}" for x in extras)
    return {
        java_path(service, area, f"Create{e}Request"): f"""package com.orbit.{service}.{area};

import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Size;
import java.math.BigDecimal;

/** What a client sends to create or update a {e}. */
public record Create{e}Request(
        @NotBlank @Size(max = 40) String code,
        @NotBlank @Size(max = 160) String name,
        @Min(0) int priority,
        BigDecimal amount,
{req}) {{
}}
""",
        java_path(service, area, f"{e}Response"): f"""package com.orbit.{service}.{area};

import java.math.BigDecimal;
import java.time.Instant;

/** What a client sees of a {e}. */
public record {e}Response(
        Long id,
        String code,
        String name,
        String status,
        int priority,
        BigDecimal amount,
        Instant createdAt,
        Instant updatedAt,
{res}) {{

    static {e}Response from({e} item) {{
        return new {e}Response(
                item.getId(),
                item.getCode(),
                item.getName(),
                item.getStatus(),
                item.getPriority(),
                item.getAmount(),
                item.getCreatedAt(),
                item.getUpdatedAt(){set_args});
    }}
}}
""",
    }


SERVICE_CREATE_CHECK = """        if (repository.existsByCode(request.code())) {
            throw new ConflictException(ErrorCode.DUPLICATE_CODE, request.code());
        }
"""
SERVICE_FIND = """        return repository.findById(id)
                .orElseThrow(() -> new NotFoundException(ErrorCode.NOT_FOUND, String.valueOf(id)));
"""
SERVICE_PRIORITY = """        if (request.priority() < 0) {
            throw new IllegalArgumentException("priority must not be negative");
        }
"""
SERVICE_TOP = """    @Transactional(readOnly = true)
    public List<%(e)sResponse> top(int limit) {
        return repository.findByStatus("ACTIVE").stream()
                .sorted(Comparator.comparingInt(%(e)s::getPriority).reversed())
                .limit(limit)
                .map(%(e)sResponse::from)
                .toList();
    }
"""


def service_java(service, e, extras):
    area = e.lower()
    sets = "".join(f"        item.set{cap(x)}(request.{x}());\n" for x in extras)
    return f"""package com.orbit.{service}.{area};

import com.orbit.common.ConflictException;
import com.orbit.common.ErrorCode;
import com.orbit.common.Limits;
import com.orbit.common.NotFoundException;
import java.math.BigDecimal;
import java.util.Comparator;
import java.util.List;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

/** The rules for working with {plural(e)}. */
@Service
public class {e}Service {{

    private static final Logger log = LoggerFactory.getLogger({e}Service.class);

    private final {e}Repository repository;

    public {e}Service({e}Repository repository) {{
        this.repository = repository;
    }}

    @Transactional
    public {e}Response create(Create{e}Request request) {{
{SERVICE_CREATE_CHECK}{SERVICE_PRIORITY}        {e} item = new {e}(request.code(), request.name());
        apply(item, request);
        {e} saved = repository.save(item);
        log.info("Created {e} {{}}", saved.getCode());
        return {e}Response.from(saved);
    }}

    @Transactional(readOnly = true)
    public {e}Response get(Long id) {{
        return {e}Response.from(find(id));
    }}

    @Transactional(readOnly = true)
    public {e}Response getByCode(String code) {{
        return repository.findByCode(code)
                .map({e}Response::from)
                .orElseThrow(() -> new NotFoundException(ErrorCode.NOT_FOUND, code));
    }}

    @Transactional(readOnly = true)
    public List<{e}Response> search(String text) {{
        return repository.search(text).stream().limit(Limits.PAGE_SIZE).map({e}Response::from).toList();
    }}

{SERVICE_TOP % {"e": e}}
    @Transactional
    public {e}Response update(Long id, Create{e}Request request) {{
        {e} item = find(id);
        if (!item.isActive()) {{
            throw new ConflictException(ErrorCode.NOT_ACTIVE, item.getCode());
        }}
        item.setName(request.name());
        apply(item, request);
        return {e}Response.from(item);
    }}

    @Transactional
    public void archive(Long id) {{
        {e} item = find(id);
        item.archive();
        log.info("Archived {e} {{}}", item.getCode());
    }}

    @Transactional(readOnly = true)
    public long countActive() {{
        return repository.countByStatus("ACTIVE");
    }}

    @Transactional(readOnly = true)
    public BigDecimal totalAmount() {{
        return repository.findByStatus("ACTIVE").stream()
                .map({e}::getAmount)
                .reduce(BigDecimal.ZERO, BigDecimal::add);
    }}

    private void apply({e} item, Create{e}Request request) {{
        item.setPriority(request.priority());
        if (request.amount() != null) {{
            item.setAmount(request.amount());
        }}
{sets}    }}

    private {e} find(Long id) {{
{SERVICE_FIND}    }}
}}
"""


def controller_java(service, e):
    area = e.lower()
    route = plural(kebab(e))
    return f"""package com.orbit.{service}.{area};

import jakarta.validation.Valid;
import java.util.List;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api/{route}")
public class {e}Controller {{

    private final {e}Service service;

    public {e}Controller({e}Service service) {{
        this.service = service;
    }}

    @PostMapping
    @ResponseStatus(HttpStatus.CREATED)
    public {e}Response create(@Valid @RequestBody Create{e}Request request) {{
        return service.create(request);
    }}

    @GetMapping("/{{id}}")
    public {e}Response get(@PathVariable Long id) {{
        return service.get(id);
    }}

    @GetMapping("/by-code/{{code}}")
    public {e}Response getByCode(@PathVariable String code) {{
        return service.getByCode(code);
    }}

    @GetMapping
    public List<{e}Response> search(@RequestParam(defaultValue = "") String text) {{
        return service.search(text);
    }}

    @GetMapping("/top")
    public List<{e}Response> top(@RequestParam(defaultValue = "10") int limit) {{
        return service.top(limit);
    }}

    @PutMapping("/{{id}}")
    public {e}Response update(@PathVariable Long id, @Valid @RequestBody Create{e}Request request) {{
        return service.update(id, request);
    }}

    @DeleteMapping("/{{id}}")
    @ResponseStatus(HttpStatus.NO_CONTENT)
    public void archive(@PathVariable Long id) {{
        service.archive(id);
    }}

    @GetMapping("/stats/active")
    public long countActive() {{
        return service.countActive();
    }}
}}
"""


def test_java(service, e, extras):
    area = e.lower()
    nulls = ", ".join(f'"{x}-value"' for x in extras)
    return f"""package com.orbit.{service}.{area};

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.orbit.common.ConflictException;
import com.orbit.common.NotFoundException;
import java.math.BigDecimal;
import java.util.Optional;
import org.junit.jupiter.api.Test;

class {e}ServiceTest {{

    private final {e}Repository repository = mock({e}Repository.class);
    private final {e}Service service = new {e}Service(repository);

    private Create{e}Request request(String code) {{
        return new Create{e}Request(code, "Sample", 3, BigDecimal.TEN, {nulls});
    }}

    @Test
    void createsWhenTheCodeIsNew() {{
        when(repository.existsByCode("A1")).thenReturn(false);
        when(repository.save(any())).thenAnswer(call -> call.getArgument(0));
        assertEquals("A1", service.create(request("A1")).code());
    }}

    @Test
    void refusesADuplicateCode() {{
        when(repository.existsByCode("A1")).thenReturn(true);
        assertThrows(ConflictException.class, () -> service.create(request("A1")));
        verify(repository, never()).save(any());
    }}

    @Test
    void reportsAMissingItem() {{
        when(repository.findById(9L)).thenReturn(Optional.empty());
        assertThrows(NotFoundException.class, () -> service.get(9L));
    }}

    @Test
    void refusesToUpdateAnArchivedItem() {{
        {e} item = new {e}("A1", "Sample");
        item.archive();
        when(repository.findById(1L)).thenReturn(Optional.of(item));
        assertThrows(ConflictException.class, () -> service.update(1L, request("A1")));
    }}
}}
"""


def client_java(caller, target, e):
    route = plural(kebab(e))
    return f"""package com.orbit.{caller}.client;

import java.math.BigDecimal;
import org.springframework.cloud.openfeign.FeignClient;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;

/** Calls the {target} service. */
@FeignClient(name = "{target}-service", path = "/api/{route}")
public interface {e}Client {{

    @GetMapping("/by-code/{{code}}")
    Remote{e} getByCode(@PathVariable("code") String code);

    @GetMapping("/stats/active")
    long countActive();

    /** The part of a {e} this service needs. */
    record Remote{e}(Long id, String code, String name, String status, BigDecimal amount) {{
    }}
}}
"""


def gateway_java(caller, target, e):
    return f"""package com.orbit.{caller}.client;

import feign.FeignException;
import java.util.Optional;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;

/** Wraps {e}Client, so a failing {target} service doesn't take this one down with it. */
@Component
public class {e}Gateway {{

    private static final Logger log = LoggerFactory.getLogger({e}Gateway.class);

    private final {e}Client client;

    public {e}Gateway({e}Client client) {{
        this.client = client;
    }}

    public Optional<{e}Client.Remote{e}> find(String code) {{
        try {{
            return Optional.of(client.getByCode(code));
        }} catch (FeignException.NotFound missing) {{
            return Optional.empty();
        }}
    }}

    public boolean isActive(String code) {{
        return find(code).map(found -> "ACTIVE".equals(found.status())).orElse(false);
    }}

    public long countActive() {{
        try {{
            return client.countActive();
        }} catch (FeignException failure) {{
            log.warn("{target} service unavailable: {{}}", failure.getMessage());
            return 0;
        }}
    }}
}}
"""


PARENT_POM = """<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>3.3.5</version>
    <relativePath/>
  </parent>
  <groupId>com.orbit</groupId>
  <artifactId>orbit-commerce</artifactId>
  <version>1.0.0-SNAPSHOT</version>
  <packaging>pom</packaging>
  <properties>
    <java.version>21</java.version>
    <spring-cloud.version>2023.0.3</spring-cloud.version>
  </properties>
  <modules>
    <module>common-lib</module>
%s  </modules>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>org.springframework.cloud</groupId>
        <artifactId>spring-cloud-dependencies</artifactId>
        <version>${spring-cloud.version}</version>
        <type>pom</type>
        <scope>import</scope>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>
"""

COMMON_POM = """<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.orbit</groupId>
    <artifactId>orbit-commerce</artifactId>
    <version>1.0.0-SNAPSHOT</version>
  </parent>
  <artifactId>common-lib</artifactId>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
  </dependencies>
</project>
"""

SERVICE_POM = """<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.orbit</groupId>
    <artifactId>orbit-commerce</artifactId>
    <version>1.0.0-SNAPSHOT</version>
  </parent>
  <artifactId>%s-service</artifactId>
  <dependencies>
    <dependency>
      <groupId>com.orbit</groupId>
      <artifactId>common-lib</artifactId>
      <version>${project.version}</version>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-data-jpa</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-validation</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework.cloud</groupId>
      <artifactId>spring-cloud-starter-openfeign</artifactId>
    </dependency>
    <dependency>
      <groupId>com.h2database</groupId>
      <artifactId>h2</artifactId>
      <scope>runtime</scope>
    </dependency>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-test</artifactId>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>
"""

ERROR_CODE = """package com.orbit.common;

/** Every error a service can report to a client. */
public enum ErrorCode {
    NOT_FOUND("The item doesn't exist"),
    DUPLICATE_CODE("That code is already in use"),
    NOT_ACTIVE("The item is archived"),
    ;

    private final String message;

    ErrorCode(String message) {
        this.message = message;
    }

    public String message() {
        return message;
    }
}
"""

LIMITS = """package com.orbit.common;

/** Limits shared by every service. */
public final class Limits {
    public static final int PAGE_SIZE = 25;
    public static final int MAX_RETRIES = 3;
    public static final long TIMEOUT_MILLIS = 2000;

    private Limits() {
    }
}
"""

RETRY = """package com.orbit.common;

/** How long to wait before trying a failed call again. */
public final class RetryPolicy {
    private RetryPolicy() {
    }

    public static long delayMillis(int attempt) {
        return 100L * attempt;
    }
}
"""

TEXT_UTIL = """package com.orbit.common;

/** Small text helpers. */
public final class TextUtil {
    private TextUtil() {
    }

    public static String normalise(String text) {
        return text == null ? "" : text.trim().toLowerCase();
    }
}
"""

LEGACY = """package com.orbit.common;

/** Dates in the old format. Nothing uses this any more. */
@Deprecated
public final class LegacyDates {
    private LegacyDates() {
    }

    public static String empty() {
        return "0000-00-00";
    }
}
"""

EXCEPTIONS = {
    "NotFoundException": "Nothing matched the request.",
    "ConflictException": "The request clashes with the current state.",
}


def exception_java(name, doc):
    return f"""package com.orbit.common;

/** {doc} */
public class {name} extends RuntimeException {{
    private final ErrorCode code;

    public {name}(ErrorCode code, String detail) {{
        super(code.message() + ": " + detail);
        this.code = code;
    }}

    public ErrorCode code() {{
        return code;
    }}
}}
"""


ADVICE = """package com.orbit.common;

import java.util.Map;
import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.ResponseStatus;
import org.springframework.web.bind.annotation.RestControllerAdvice;

/** Turns exceptions into the error body clients expect. */
@RestControllerAdvice
public class ApiExceptionHandler {

    @ExceptionHandler(NotFoundException.class)
    @ResponseStatus(HttpStatus.NOT_FOUND)
    public Map<String, String> notFound(NotFoundException failure) {
        return Map.of("code", failure.code().name(), "message", failure.getMessage());
    }

    @ExceptionHandler(ConflictException.class)
    @ResponseStatus(HttpStatus.CONFLICT)
    public Map<String, String> conflict(ConflictException failure) {
        return Map.of("code", failure.code().name(), "message", failure.getMessage());
    }
}
"""


def application_java(service):
    name = cap(service) + "ServiceApplication"
    return name, f"""package com.orbit.{service};

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.cloud.openfeign.EnableFeignClients;

@SpringBootApplication(scanBasePackages = {{"com.orbit.{service}", "com.orbit.common"}})
@EnableFeignClients
public class {name} {{
    public static void main(String[] args) {{
        SpringApplication.run({name}.class, args);
    }}
}}
"""


# ---------------------------------------------------------------- planted bugs
# Each one replaces an exact piece of the service and is a real defect.

def bugs_for(e):
    return [
        ("console-output", "Prints to the console instead of logging", f'        log.info("Created {e} {{}}", saved.getCode());\n', f'        System.out.println("Created " + saved.getCode());\n'),
        ("optional-get", "Calls Optional.get() with no check, so a missing id gives the wrong error", SERVICE_FIND, "        return repository.findById(id).get();\n"),
        ("string-equals", "Compares strings with ==, so the archived check never matches", "        if (!item.isActive()) {\n", '        if (item.getStatus() == "ARCHIVED") {\n'),
        ("inverted-check", "The negative check is inverted, so valid priorities are refused", "request.priority() < 0", "request.priority() > 0"),
        ("check-then-act", "Removes the existence check, so a duplicate code fails at the database instead", SERVICE_CREATE_CHECK, ""),
        ("swallowed-exception", "Swallows a failure and reports success", f"        item.archive();\n        log.info(\"Archived {e} {{}}\", item.getCode());\n", "        try {\n            item.archive();\n        } catch (RuntimeException ignored) {\n        }\n"),
        ("bigdecimal-double", "Builds a BigDecimal from a double, which loses precision", "            item.setAmount(request.amount());\n", "            item.setAmount(new BigDecimal(request.amount().doubleValue()));\n"),
        ("hard-coded-secret", "Puts a credential in the source", "    private final " + e + "Repository repository;\n", '    private static final String API_KEY = "orbit-demo-secret-not-real-123";\n\n    private final ' + e + "Repository repository;\n"),
        ("off-by-one", "Returns one more item than asked for", ".limit(limit)", ".limit(limit + 1)"),
        ("self-invocation", "Calls a transactional method on this, which skips the transaction", "    public void archive(Long id) {\n", f"    public void archiveQuietly(Long id) {{\n        this.update(id, null);\n    }}\n\n    @Transactional\n    public void archive(Long id) {{\n"),
    ]


# ---------------------------------------------------------------- the simulation

class Team:
    def __init__(self, root, pace, seed):
        self.root, self.pace = os.path.abspath(os.path.expanduser(root)), pace
        self.remote = self.root + ".git"
        self.notes = self.root + "-notes"
        self.rng = random.Random(seed)
        self.pr = 0
        self.dev = DEVS[0]
        self.stats = {"commits": 0, "prs": 0, "conflicts": 0, "reverts": 0, "force_pushes": 0, "seeded_bugs": 0, "bugs_fixed": 0}
        self.open_bugs = []

    def say(self, text):
        line = time.strftime("%H:%M:%S ") + text
        print(line, flush=True)
        with open(os.path.join(self.notes, "timeline.log"), "a", encoding="utf-8") as f:
            f.write(line + "\n")

    def wait(self, factor=1.0):
        if self.pace:
            time.sleep(self.pace * factor)

    def git(self, *args, check=True, dev=None):
        name, email = dev or self.dev
        env = dict(os.environ, GIT_AUTHOR_NAME=name, GIT_AUTHOR_EMAIL=email, GIT_COMMITTER_NAME=name, GIT_COMMITTER_EMAIL=email, GIT_EDITOR="true")
        r = subprocess.run(["git", "-C", self.root, *args], env=env, capture_output=True, text=True)
        if check and r.returncode != 0:
            raise RuntimeError(f"git {' '.join(args)}: {r.stdout}{r.stderr}")
        return r

    def write(self, path, text):
        full = os.path.join(self.root, path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w", encoding="utf-8", newline="\n") as f:
            f.write(text)

    def read(self, path):
        with open(os.path.join(self.root, path), encoding="utf-8") as f:
            return f.read()

    def replace(self, path, old, new):
        text = self.read(path)
        if old not in text:
            raise RuntimeError(f"{path}: expected text is missing: {old!r}")
        self.write(path, text.replace(old, new, 1))

    def commit(self, message, wait=1.0):
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)
        self.stats["commits"] += 1
        self.say(f"{self.dev[0]}: {message.splitlines()[0]}")
        self.wait(wait)

    def branch(self, name, base="main"):
        self.git("checkout", "-q", base)
        self.git("checkout", "-q", "-b", name)

    def push(self, branch, force=False):
        self.git("push", "-q", "origin", branch, *(["--force-with-lease"] if force else []))

    def resolve_keep_both(self, path):
        lines = [l for l in self.read(path).split("\n") if not re.match(r"^(<<<<<<<|=======|>>>>>>>)", l)]
        self.write(path, "\n".join(lines))

    def merge_pr(self, branch, title, reviewer=None, resolve=None, style="merge", delete=True):
        """Merges like a pull request. A conflict stays open for a while, so it can be seen in the UI."""
        self.pr += 1
        self.stats["prs"] += 1
        author = self.dev
        merger = reviewer or self.rng.choice([d for d in DEVS if d != author])
        self.git("checkout", "-q", "main", dev=merger)
        self.git("pull", "-q", "--ff-only", "origin", "main", check=False, dev=merger)
        self.dev = merger
        message = f"Merge pull request #{self.pr} from orbit/{branch}\n\n{title}\n\nReviewed-by: {merger[0]} <{merger[1]}>"
        if style == "squash":
            self.git("merge", "-q", "--squash", branch)
            self.commit(f"{title} (#{self.pr})")
        elif style == "ff":
            self.git("merge", "-q", "--ff-only", branch)
            self.say(f"{merger[0]}: fast-forwarded main over {branch} (#{self.pr})")
            self.wait()
        else:
            r = self.git("merge", "--no-ff", "-m", message, branch, check=False)
            if r.returncode != 0:
                self.stats["conflicts"] += 1
                files = self.git("diff", "--name-only", "--diff-filter=U").stdout.split()
                self.say(f"{merger[0]}: CONFLICT merging {branch} in {', '.join(files)}")
                self.wait(3)
                for path in files:
                    (resolve or self.resolve_keep_both)(path)
                self.git("add", "-A")
                self.git("commit", "-q", "-m", message)
                self.stats["commits"] += 1
                self.say(f"{merger[0]}: resolved the conflict and merged #{self.pr}")
            else:
                self.stats["commits"] += 1
                self.say(f"{merger[0]}: merged pull request #{self.pr}: {title}")
            self.wait()
        self.push("main")
        if delete:
            self.git("branch", "-q", "-D", branch)
            self.git("push", "-q", "origin", "--delete", branch, check=False)
        self.dev = author

    # -------------------------------------------------- building

    def bootstrap(self):
        os.makedirs(self.notes, exist_ok=True)
        subprocess.run(["git", "init", "-q", "--bare", "-b", "main", self.remote], check=True)
        os.makedirs(self.root, exist_ok=True)
        subprocess.run(["git", "init", "-q", "-b", "main", self.root], check=True)
        self.git("remote", "add", "origin", self.remote)
        modules = "".join(f"    <module>{s}-service</module>\n" for s in SERVICES)
        self.write("pom.xml", PARENT_POM % modules)
        self.write("common-lib/pom.xml", COMMON_POM)
        self.write(".gitignore", "target/\n.idea/\n*.iml\n")
        self.write("README.md", "# Orbit Commerce\n\nAn online store built as Spring Boot microservices.\n")
        self.write("CHANGELOG.md", "# Changelog\n\n## Unreleased\n\n- Project set up\n")
        common = "common-lib/src/main/java/com/orbit/common/"
        self.write(common + "ErrorCode.java", ERROR_CODE)
        self.write(common + "Limits.java", LIMITS)
        self.write(common + "RetryPolicy.java", RETRY)
        self.write(common + "TextUtil.java", TEXT_UTIL)
        self.write(common + "LegacyDates.java", LEGACY)
        self.write(common + "ApiExceptionHandler.java", ADVICE)
        for name, doc in EXCEPTIONS.items():
            self.write(common + name + ".java", exception_java(name, doc))
        self.commit("Set up the project and the shared library", wait=0.5)
        for service in SERVICES:
            self.write(f"{service}-service/pom.xml", SERVICE_POM % service)
            name, text = application_java(service)
            self.write(java_path(service, "", name).replace("//", "/").replace("/" + name, "/" + name), text)
            self.write(f"{service}-service/src/main/resources/application.yml", f"spring:\n  application:\n    name: {service}-service\n  jpa:\n    open-in-view: false\n  datasource:\n    url: jdbc:h2:mem:{service}\n")
        self.commit("Add the service modules", wait=0.5)
        self.push("main")

    def add_clients(self, caller):
        for target, e in CALLS.get(caller, []):
            self.write(java_path(caller, "client", f"{e}Client"), client_java(caller, target, e))
            self.write(java_path(caller, "client", f"{e}Gateway"), gateway_java(caller, target, e))
        if CALLS.get(caller):
            self.commit(f"Add clients for the services {caller} depends on")

    def feature_steps(self, service, e, extras, bug):
        """The commits of one feature, as a list of functions. A planted bug arrives with the service."""
        area = e.lower()

        def model():
            self.write(java_path(service, area, e), entity_java(service, e, extras))
            self.write(java_path(service, area, f"{e}Repository"), repository_java(service, e))
            self.commit(f"Add the {e} model and repository")

        def logic():
            for path, text in dtos_java(service, e, extras).items():
                self.write(path, text)
            text = service_java(service, e, extras)
            if bug:
                bug_id, why, old, new = bug
                if old not in text:
                    raise RuntimeError(f"bug {bug_id} doesn't fit {e}")
                text = text.replace(old, new, 1)
            self.write(java_path(service, area, f"{e}Service"), text)
            self.commit(f"Add {e} business rules")
            if bug:
                sha = self.git("rev-parse", "HEAD").stdout.strip()
                self.stats["seeded_bugs"] += 1
                with open(os.path.join(self.notes, "seeded-bugs.jsonl"), "a", encoding="utf-8") as f:
                    f.write(json.dumps({"bug": bug_id, "why": why, "entity": e, "service": service, "commit": sha, "file": java_path(service, area, f"{e}Service")}) + "\n")
                self.open_bugs.append((service, e, extras, bug_id))

        def web():
            self.write(java_path(service, area, f"{e}Controller"), controller_java(service, e))
            self.commit(f"Expose {plural(e)} over REST")

        def tests():
            self.write(java_path(service, area, f"{e}ServiceTest", test=True), test_java(service, e, extras))
            self.commit(f"Test the {e} rules")

        return [model, logic, web, tests]

    def fix_bug(self):
        """A later pull request repairs one planted bug, by writing the correct service back."""
        if not self.open_bugs:
            return
        service, e, extras, bug_id = self.open_bugs.pop(0)
        self.dev = self.rng.choice(DEVS)
        name = f"fix/{kebab(e)}-{bug_id}"
        self.branch(name)
        self.write(java_path(service, e.lower(), f"{e}Service"), service_java(service, e, extras))
        self.commit(f"Fix {bug_id.replace('-', ' ')} in {e}Service")
        self.push(name)
        self.merge_pr(name, f"Fix {bug_id.replace('-', ' ')} in {e}Service")
        self.stats["bugs_fixed"] += 1

    def changelog(self, text):
        self.replace("CHANGELOG.md", "## Unreleased\n\n", f"## Unreleased\n\n- {text}\n")

    # -------------------------------------------------- the things that happen on a team

    def scene_enum_conflict(self):
        common = "common-lib/src/main/java/com/orbit/common/ErrorCode.java"
        a, b = self.rng.sample(DEVS, 2)
        for dev, branch, line in ((a, "feature/payment-errors", '    PAYMENT_DECLINED("The payment was declined"),'), (b, "feature/stock-errors", '    OUT_OF_STOCK("The item is out of stock"),')):
            self.dev = dev
            self.branch(branch)
            self.replace(common, "    ;\n", line + "\n    ;\n")
            self.commit(f"Add the {line.split('(')[0].strip()} error code")
            self.push(branch)
        self.dev = a
        self.merge_pr("feature/payment-errors", "Add the payment error code")
        self.dev = b
        self.merge_pr("feature/stock-errors", "Add the stock error code")

    def scene_same_line_conflict(self):
        path = "common-lib/src/main/java/com/orbit/common/Limits.java"
        a, b = self.rng.sample(DEVS, 2)
        for dev, branch, size in ((a, "tune/page-size-50", 50), (b, "tune/page-size-100", 100)):
            self.dev = dev
            self.branch(branch)
            self.write(path, re.sub(r"PAGE_SIZE = \d+", f"PAGE_SIZE = {size}", self.read(path)))
            self.commit(f"Raise the page size to {size}")
            self.push(branch)
        self.dev = a
        self.merge_pr("tune/page-size-50", "Raise the page size to 50")
        self.dev = b

        def choose(p):
            text = re.sub(r"<<<<<<<.*?\n(.*?)=======\n(.*?)>>>>>>>.*?\n", r"\2", self.read(p), flags=re.S)
            self.write(p, text)
        self.merge_pr("tune/page-size-100", "Raise the page size to 100", resolve=choose)

    def scene_clean_merge_breaks_build(self):
        """Both merges succeed in Git, and together they don't compile. Only review or a build can see this."""
        path = java_path("order", "pricing", "OrderPricing")
        a, b = self.rng.sample(DEVS, 2)
        self.dev = a
        self.branch("feature/order-pricing")
        self.write(path, """package com.orbit.order.pricing;

import java.math.BigDecimal;
import org.springframework.stereotype.Component;

@Component
public class OrderPricing {

    public BigDecimal total(BigDecimal net, BigDecimal tax) {
        return net.add(tax);
    }
}
""")
        self.commit("Add order pricing")
        self.push("feature/order-pricing")
        self.merge_pr("feature/order-pricing", "Add order pricing")
        self.dev = a
        self.branch("refactor/gross-total")
        self.dev = b
        self.branch("feature/order-quote")
        self.write(java_path("order", "pricing", "QuotePricing"), """package com.orbit.order.pricing;

import java.math.BigDecimal;
import org.springframework.stereotype.Component;

@Component
public class QuotePricing {

    private final OrderPricing pricing;

    public QuotePricing(OrderPricing pricing) {
        this.pricing = pricing;
    }

    public BigDecimal quote(BigDecimal net, BigDecimal tax) {
        return pricing.total(net, tax);
    }
}
""")
        self.commit("Price quotes through OrderPricing")
        self.push("feature/order-quote")
        self.dev = a
        self.git("checkout", "-q", "refactor/gross-total")
        self.replace(path, "public BigDecimal total(", "public BigDecimal grossTotal(")
        self.commit("Rename total to grossTotal")
        self.push("refactor/gross-total")
        self.merge_pr("refactor/gross-total", "Rename total to grossTotal")
        self.dev = b
        self.merge_pr("feature/order-quote", "Price quotes through OrderPricing")
        self.say("main no longer compiles: a clean merge called a method another branch renamed")
        self.wait(2)
        self.git("checkout", "-q", "main")
        self.dev = a
        self.replace(java_path("order", "pricing", "QuotePricing"), "pricing.total(net, tax)", "pricing.grossTotal(net, tax)")
        self.commit("Fix the build: call grossTotal from QuotePricing")
        self.push("main")

    def scene_revert(self):
        self.dev = self.rng.choice(DEVS)
        path = "common-lib/src/main/java/com/orbit/common/Limits.java"
        self.branch("tune/short-timeout")
        self.write(path, re.sub(r"TIMEOUT_MILLIS = \d+", "TIMEOUT_MILLIS = 50", self.read(path)))
        self.commit("Lower the timeout to speed up responses")
        self.push("tune/short-timeout")
        self.merge_pr("tune/short-timeout", "Lower the timeout")
        self.say("the short timeout is failing calls in production, so it gets reverted")
        self.wait()
        self.git("checkout", "-q", "main")
        self.dev = self.rng.choice(DEVS)
        self.git("revert", "--no-edit", "-m", "1", "HEAD")
        self.stats["commits"] += 1
        self.stats["reverts"] += 1
        self.say(f"{self.dev[0]}: reverted the timeout change")
        self.push("main")
        self.wait()

    def scene_release_and_hotfix(self):
        self.git("checkout", "-q", "main")
        self.dev = DEVS[0]
        self.changelog("First release")
        self.commit("Prepare release 1.0.0")
        self.git("tag", "-a", "v1.0.0", "-m", "Release 1.0.0")
        self.git("branch", "release/1.0")
        self.git("push", "-q", "origin", "main", "release/1.0", "v1.0.0")
        self.say("tagged v1.0.0 and cut release/1.0")
        self.wait()
        self.dev = self.rng.choice(DEVS)
        self.branch("hotfix/retry-backoff", base="release/1.0")
        path = "common-lib/src/main/java/com/orbit/common/RetryPolicy.java"
        self.replace(path, "return 100L * attempt;", "return 100L * (1L << Math.min(attempt, 6));")
        self.commit("Back off exponentially between retries")
        sha = self.git("rev-parse", "HEAD").stdout.strip()
        self.git("checkout", "-q", "release/1.0")
        self.git("merge", "-q", "--no-ff", "-m", "Merge hotfix/retry-backoff into release/1.0", "hotfix/retry-backoff")
        self.stats["commits"] += 1
        self.git("tag", "-a", "v1.0.1", "-m", "Release 1.0.1")
        self.git("push", "-q", "origin", "release/1.0", "v1.0.1")
        self.say(f"{self.dev[0]}: released the hotfix as v1.0.1")
        self.wait()
        self.git("checkout", "-q", "main")
        self.git("cherry-pick", "-x", sha)
        self.stats["commits"] += 1
        self.say(f"{self.dev[0]}: cherry-picked the hotfix onto main")
        self.push("main")
        self.git("branch", "-q", "-D", "hotfix/retry-backoff")
        self.wait()

    def scene_rebase_with_conflict(self):
        a, b = self.rng.sample(DEVS, 2)
        self.dev = a
        self.branch("feature/rebase-notes")
        self.changelog("Clearer error messages")
        self.commit("Note the clearer error messages")
        self.push("feature/rebase-notes")
        self.git("checkout", "-q", "main")
        self.dev = b
        self.changelog("Faster product search")
        self.commit("Note the faster product search")
        self.push("main")
        self.dev = a
        self.git("checkout", "-q", "feature/rebase-notes")
        r = self.git("rebase", "main", check=False)
        if r.returncode != 0:
            self.stats["conflicts"] += 1
            self.say(f"{a[0]}: CONFLICT while rebasing feature/rebase-notes onto main")
            self.wait(3)
            self.resolve_keep_both("CHANGELOG.md")
            self.git("add", "-A")
            self.git("rebase", "--continue")
            self.say(f"{a[0]}: finished the rebase")
        self.push("feature/rebase-notes", force=True)
        self.stats["force_pushes"] += 1
        self.merge_pr("feature/rebase-notes", "Note the clearer error messages")

    def scene_amend_force_push(self):
        self.dev = self.rng.choice(DEVS)
        self.branch("docs/readme-services")
        self.replace("README.md", "microservices.\n", "microservices.\n\nRun a service with mvn spring-boot:run.\n")
        self.commit("Docmuent how to run")
        self.push("docs/readme-services")
        self.git("commit", "-q", "--amend", "-m", "Document how to run a service")
        self.say(f"{self.dev[0]}: fixed a typo in the commit message and force-pushed")
        self.push("docs/readme-services", force=True)
        self.stats["force_pushes"] += 1
        self.wait()
        self.merge_pr("docs/readme-services", "Document how to run a service")

    def scene_squash(self):
        self.dev = self.rng.choice(DEVS)
        self.branch("feature/readme-layout")
        self.replace("README.md", "# Orbit Commerce\n", "# Orbit Commerce\n\nSee CHANGELOG.md for what changed.\n")
        self.commit("wip", wait=0.5)
        self.replace("README.md", "See CHANGELOG.md for what changed.\n", "See CHANGELOG.md for each release.\n")
        self.commit("wip 2", wait=0.5)
        self.push("feature/readme-layout")
        self.merge_pr("feature/readme-layout", "Link the changelog from the README", style="squash")

    def scene_fast_forward(self):
        self.dev = self.rng.choice(DEVS)
        self.branch("chore/ignore-logs")
        self.replace(".gitignore", "target/\n", "target/\n*.log\n")
        self.commit("Ignore log files")
        self.push("chore/ignore-logs")
        self.merge_pr("chore/ignore-logs", "Ignore log files", style="ff")

    def scene_rename(self):
        self.dev = self.rng.choice(DEVS)
        self.branch("refactor/strings")
        common = "common-lib/src/main/java/com/orbit/common/"
        self.git("mv", common + "TextUtil.java", common + "Strings.java")
        self.replace(common + "Strings.java", "class TextUtil", "class Strings")
        self.replace(common + "Strings.java", "private TextUtil()", "private Strings()")
        self.commit("Rename TextUtil to Strings")
        self.push("refactor/strings")
        self.merge_pr("refactor/strings", "Rename TextUtil to Strings")

    def scene_delete(self):
        self.dev = self.rng.choice(DEVS)
        self.branch("chore/remove-legacy-dates")
        self.git("rm", "-q", "common-lib/src/main/java/com/orbit/common/LegacyDates.java")
        self.commit("Remove LegacyDates, which nothing uses")
        self.push("chore/remove-legacy-dates")
        self.merge_pr("chore/remove-legacy-dates", "Remove LegacyDates")

    def scene_direct_commit(self):
        self.git("checkout", "-q", "main")
        self.dev = self.rng.choice(DEVS)
        self.changelog("Clearer logs on startup")
        self.commit("Update the changelog")
        self.push("main")

    def scene_uncommitted_work(self):
        """Edits sit uncommitted for a while, so the working changes can be seen."""
        self.git("checkout", "-q", "main")
        self.dev = self.rng.choice(DEVS)
        self.branch("feature/retry-limit")
        path = "common-lib/src/main/java/com/orbit/common/Limits.java"
        self.write(path, re.sub(r"MAX_RETRIES = \d+", "MAX_RETRIES = 5", self.read(path)))
        self.say(f"{self.dev[0]}: has uncommitted changes in Limits.java")
        self.wait(3)
        self.commit("Allow five retries")
        self.push("feature/retry-limit")
        self.merge_pr("feature/retry-limit", "Allow five retries")

    def scene_stash(self):
        self.git("checkout", "-q", "main")
        self.dev = self.rng.choice(DEVS)
        self.replace("README.md", "# Orbit Commerce\n", "# Orbit Commerce (draft)\n")
        self.git("stash", "push", "-q", "-m", "README draft")
        self.say(f"{self.dev[0]}: stashed a draft to deal with something urgent")
        self.changelog("Urgent note about the outage")
        self.commit("Note the outage in the changelog")
        self.push("main")
        self.git("stash", "pop", "-q")
        self.say(f"{self.dev[0]}: brought the draft back")
        self.wait()
        self.git("checkout", "-q", "--", "README.md")

    def scene_merge_main_into_feature(self):
        a = self.rng.choice(DEVS)
        self.dev = a
        self.branch("feature/long-running-docs")
        self.replace("README.md", "# Orbit Commerce\n", "# Orbit Commerce\n\nOwned by the platform team.\n")
        self.commit("Say who owns the project")
        self.push("feature/long-running-docs")
        self.git("checkout", "-q", "main")
        self.changelog("Better search ranking")
        self.commit("Note the search ranking")
        self.push("main")
        self.git("checkout", "-q", "feature/long-running-docs")
        self.git("merge", "--no-edit", "-q", "main")
        self.stats["commits"] += 1
        self.say(f"{a[0]}: merged main into feature/long-running-docs to catch up")
        self.push("feature/long-running-docs")
        self.wait()
        self.merge_pr("feature/long-running-docs", "Say who owns the project")

    def scene_open_branches(self):
        """Branches left open, so branch warnings have something to show. One will conflict with main."""
        path = "common-lib/src/main/java/com/orbit/common/Limits.java"
        self.dev = self.rng.choice(DEVS)
        self.branch("experiment/page-size-10")
        self.write(path, re.sub(r"PAGE_SIZE = \d+", "PAGE_SIZE = 10", self.read(path)))
        self.commit("Try a smaller page size")
        self.push("experiment/page-size-10")
        self.dev = self.rng.choice(DEVS)
        self.branch("feature/gift-cards")
        self.write(java_path("payment", "giftcard", "GiftCardRules"), """package com.orbit.payment.giftcard;

import java.math.BigDecimal;

/** Early draft, not used yet. */
public final class GiftCardRules {
    private GiftCardRules() {
    }

    public static boolean covers(BigDecimal balance, BigDecimal amount) {
        return balance.compareTo(amount) >= 0;
    }
}
""")
        self.commit("Start on gift cards")
        self.push("feature/gift-cards")
        self.git("checkout", "-q", "main")

    def scene_pull_burst(self):
        """Several commits land at once, as after a long pull."""
        keep = self.pace
        self.pace = 0
        self.git("checkout", "-q", "main")
        self.dev = self.rng.choice(DEVS)
        for i in range(1, 7):
            self.changelog(f"Small improvement {i}")
            self.commit(f"Record small improvement {i}")
        self.push("main")
        self.pace = keep
        self.say("six commits landed on main at once")
        self.wait()

    # -------------------------------------------------- the run

    def run(self, limit=None):
        self.bootstrap()
        self.rng.seed(7)
        work = [(s, e, x) for s, es in SERVICES.items() for e, x in es.items()]
        self.rng.shuffle(work)
        if limit:
            work = work[:limit]
        scenes = [self.scene_enum_conflict, self.scene_direct_commit, self.scene_same_line_conflict, self.scene_uncommitted_work,
                  self.scene_clean_merge_breaks_build, self.scene_squash, self.scene_revert, self.scene_fast_forward,
                  self.scene_release_and_hotfix, self.scene_rename, self.scene_rebase_with_conflict, self.scene_stash,
                  self.scene_amend_force_push, self.scene_delete, self.scene_merge_main_into_feature, self.scene_pull_burst,
                  self.scene_open_branches]
        clients_done = set()
        n = 0
        bug_index = 0
        for i in range(0, len(work), 2):
            pair = work[i:i + 2]
            # Two developers work at the same time, so their commits interleave on separate branches.
            streams = []
            for service, e, extras in pair:
                dev = self.rng.choice(DEVS)
                n += 1
                bug = None
                if n % 3 == 0:
                    catalogue = bugs_for(e)
                    bug = catalogue[bug_index % len(catalogue)]
                    bug_index += 1
                name = f"feature/{service}-{kebab(e)}"
                self.dev = dev
                self.branch(name)
                if service not in clients_done:
                    clients_done.add(service)
                    self.add_clients(service)
                streams.append((dev, name, service, e, self.feature_steps(service, e, extras, bug)))
            for step in range(4):
                for dev, name, _, _, steps in streams:
                    self.dev = dev
                    self.git("checkout", "-q", name)
                    steps[step]()
            for dev, name, service, e, _ in streams:
                self.dev = dev
                self.git("checkout", "-q", name)
                self.push(name)
                self.merge_pr(name, f"Add {plural(e)} to the {service} service")
            if i % 6 == 4 and self.open_bugs and self.rng.random() < 0.7:
                self.fix_bug()
            if i % 4 == 2 and scenes:
                scenes.pop(0)()
        while scenes:
            scenes.pop(0)()
        self.git("checkout", "-q", "main")
        self.finish()

    def finish(self):
        files = self.git("ls-files", "*.java").stdout.split()
        lines = sum(self.read(f).count("\n") for f in files)
        summary = {**self.stats, "javaFiles": len(files), "javaLines": lines, "stillOpenBugs": len(self.open_bugs),
                   "branches": self.git("branch", "--format=%(refname:short)").stdout.split()}
        with open(os.path.join(self.notes, "summary.json"), "w", encoding="utf-8") as f:
            json.dump(summary, f, indent=1)
        self.say("finished: " + json.dumps(summary))


def main():
    ap = argparse.ArgumentParser(description="Simulate a team building a Java project.")
    ap.add_argument("project", help="folder to create; it must not exist")
    ap.add_argument("--pace", type=float, default=20, help="seconds between actions")
    ap.add_argument("--seed", type=int, default=7)
    ap.add_argument("--limit", type=int, help="build only this many features")
    ap.add_argument("--check", action="store_true", help="compile the result with Maven at the end")
    args = ap.parse_args()
    root = os.path.abspath(os.path.expanduser(args.project))
    if os.path.exists(root):
        raise SystemExit(f"{root} already exists")
    team = Team(root, args.pace, args.seed)
    team.run(args.limit)
    if args.check:
        r = subprocess.run(["mvn", "-q", "-T4", "-DskipTests", "test-compile"], cwd=root, capture_output=True, text=True)
        print((r.stdout + r.stderr)[-2500:])
        print("compiled" if r.returncode == 0 else "COMPILE FAILED")
        shutil.rmtree(os.path.join(root, "target"), ignore_errors=True)
        sys.exit(r.returncode)


if __name__ == "__main__":
    main()
