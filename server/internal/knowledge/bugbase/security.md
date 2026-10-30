# Security

## Hard-coded secrets {#hardcoded-secret}
severity: critical
category: security
tags: password secret apiKey token key credentials private key String literal properties
see: Sonar S2068 S6418

Passwords, API keys and tokens in source or committed config end up in every clone, build artifact and log
of the repository forever. Read them from the environment or a secret manager.

## Secrets or personal data in logs {#sensitive-data-logged}
severity: high
category: security
tags: log password token card number email request body toString PII GDPR
see: Sonar S5145 (log injection), OWASP logging

Logging a request object, an entity `toString()` or headers often writes passwords, tokens, card numbers
and personal data to log storage that many people can read. Log ids, not payloads.

## Log injection {#log-injection}
severity: medium
category: security
tags: log user input newline CRLF forged log entries
see: Sonar S5145

User input logged verbatim can contain line breaks that forge fake log entries or break log parsing.
Structured logging (key/value) or sanitising control characters prevents it.

## Command injection {#command-injection}
severity: critical
category: security
tags: Runtime.exec ProcessBuilder shell bash -c user input
see: Sonar S2076

Passing user input into a shell command lets an attacker run any command. Use `ProcessBuilder` with a fixed
executable and separate arguments, never `sh -c` with concatenated input.

## Path traversal {#path-traversal}
severity: critical
category: security
tags: File Paths.get user input filename upload download ../ normalize
see: Sonar S2083

A file name from a request (`../../etc/passwd`) can escape the intended directory. Normalise the resolved path
and check it still starts with the base directory; better, store uploads under generated names.

## Server-side request forgery {#ssrf}
severity: high
category: security
tags: URL user input RestTemplate WebClient fetch webhook callback internal metadata
see: Sonar S5144

Fetching a URL supplied by the user lets attackers reach internal services and cloud metadata endpoints.
Allow-list hosts and schemes.

## Unsafe deserialization {#unsafe-deserialization}
severity: critical
category: security
tags: ObjectInputStream readObject Jackson enableDefaultTyping polymorphic XMLDecoder
see: Sonar S5135

Java deserialization of untrusted data and Jackson default typing allow remote code execution through gadget
chains. Never deserialize untrusted bytes with `ObjectInputStream`; with Jackson use explicit types.

## XML external entities {#xxe}
severity: high
category: security
tags: DocumentBuilderFactory SAXParser XMLInputFactory external entity DTD XXE
see: Sonar S2755

Default XML parsers resolve external entities: an uploaded XML file can read local files or call internal
URLs. Disable DTDs and external entities on the factory.

## Missing authorization on an endpoint {#missing-authorization}
severity: high
category: security
tags: @PreAuthorize @Secured hasRole controller admin endpoint ownership IDOR userId path variable
see: OWASP A01 Broken Access Control

An endpoint that loads a resource by id from the URL without checking that the caller owns it (or has the
role) lets any authenticated user read or change other users' data (IDOR). Check ownership in the service.

## CSRF disabled or CORS wide open {#csrf-cors}
severity: high
category: security
tags: csrf disable cors allowedOrigins * allowCredentials SecurityFilterChain
see: Sonar S4502 S5122

`csrf().disable()` is right for stateless token APIs and wrong for cookie sessions. `allowedOrigins("*")`
with credentials lets any site act as the user.

## Weak hashing or encryption {#weak-crypto}
severity: high
category: security
tags: MD5 SHA-1 password hash DES ECB AES Cipher BCrypt PBKDF2 encoder
see: Sonar S4790 S5547 S5542 S2070

MD5/SHA-1 for passwords, unsalted hashes, DES, and AES in ECB mode are broken. Passwords need a slow,
salted hash (`BCryptPasswordEncoder`, Argon2); data needs AES-GCM with a random IV.

## Disabled TLS verification {#trust-all-tls}
severity: critical
category: security
tags: TrustManager X509TrustManager HostnameVerifier trust all certificates SSLContext
see: Sonar S4830 S5527

A trust-all `TrustManager` or a hostname verifier that returns true makes every HTTPS call open to
man-in-the-middle attacks, usually added "temporarily" for a self-signed test certificate.

## Mass assignment {#mass-assignment}
severity: medium
category: security
tags: @RequestBody entity role admin isAdmin balance binding DTO
see: OWASP API6

Binding a request directly onto an entity lets clients set fields they should not (`role`, `balance`,
`verified`). Accept a request DTO with only the editable fields.

## Exception details returned to clients {#error-detail-leak}
severity: medium
category: security
tags: stack trace e.getMessage response body server.error.include-stacktrace SQL message
see: Sonar S4507

Returning exception messages or stack traces exposes SQL, class names and internal hosts. Return a generic
message with a correlation id and log the details.
