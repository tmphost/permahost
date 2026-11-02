# tmphost Backend Coding Style and Naming Conventions

This document outlines the coding style and naming conventions for the tmphost backend project. AI assistants (like Gemini) should strictly adhere to these guidelines when generating or modifying code.

## 1. Project Structure

The project follows a standard Go layered architecture:
- `internal/handler/{transport}`: Transport layer handlers (e.g., Fiber HTTP handlers in `api`).
- `internal/controller/{domain}`: Business logic implementation (e.g., CRUD actions or domain-specific modules).
- `internal/data/model`: Database models (GORM) and table definitions.
- `internal/data/entity`: Domain entities passed between layers.
- `internal/data/view`: External representations (e.g., JSON request/response bodies).
- `internal/mapper`: Functions to map between models, entities, and views.
- `internal/repository`: Data access layer.
- `internal/service`: Service layer.

## 2. Naming Conventions

### Handlers (`internal/handler/{transport}/`)
Handlers process incoming HTTP requests and format responses.

- **Naming**: File names should match the entity being handled (e.g., `provider_tool.go`).
- **Structs**: The handler struct is typically named after the entity, e.g., `type ProviderTool struct { controller _crud.ProviderToolController }`.
- **Methods**: Use standard CRUD terminology: `Create`, `Get`, `List`, `Update`, `Delete`. They should match the Fiber v3 `fiber.Handler` signature `func(c fiber.Ctx) error`; use the latest Fiber (v3) for every HTTP path, including the http-to-https redirect.
- **Flow**:
  1. Use a mapper to parse the request into an entity (e.g., `reqEntity := api.FromHTTPRequestTo<Action><Entity>RequestEntity(c)`).
  2. Return `fiber.ErrBadRequest` if the mapper returns `nil` (meaning parsing failed or a required param was missing).
  3. Call the corresponding `controller` method with `c.Context()` and the parsed request entity.
  4. Handle domain errors gracefully (e.g., checking for specific error types and returning appropriate HTTP status codes).
  5. Use a mapper to format the controller's response entity into a JSON payload byte array.
  6. Set `fiber.HeaderContentType` to `fiber.MIMEApplicationJSON` before returning `c.Send(payload)`.

### Controllers (`internal/controller/{domain}/`)
Controllers contain the core business logic.

- **Naming**: File names match the entity or domain logic (e.g., `provider_tool.go`).
- **Interface**: Define a `Controller` interface at the top of the file grouping the supported operations.
- **Implementation**: The struct implementing the interface should handle repository interactions, business rules, and validation.
- **Methods**: Methods typically accept a `context.Context` and a specific request entity (e.g., `*entity.CreateProviderToolRequest`), and return a specific response entity (e.g., `*entity.CreateProviderToolResponse`, `error`).

### Mappers (`internal/mapper/{transport}/`, `internal/mapper/model/`)
Mappers transform data between the different layers (View <-> Entity <-> Model). Focus on explicitly naming the source and target formats. Mappers must not return error. It can return `nil` if the conversion is not possible. The validation should be done in the controller/service layers.

- **Naming Constraints**:
  - Mapping mapped HTTP requests to Entities start with `FromHTTPRequestTo` and end with `RequestEntity`.
    - Example: `func FromHTTPRequestToCreateProviderToolRequestEntity(c fiber.Ctx) *entity.CreateProviderToolRequest`
  - Mapping Entities to HTTP responses start with `From` and end with `ResponseEntityToHTTPResponse` (returning `[]byte`).
    - Example: `func FromCreateProviderToolResponseEntityToHTTPResponse(rs *entity.CreateProviderToolResponse) []byte`
  - Mapping internal structures start with `From` and explicitly state the source and target formats.
    - Example: `func FromProviderToolViewToProviderToolEntity(e view.ProviderTool) entity.ProviderTool`
    - Example: `func FromProviderToolEntitiesToProviderToolViews(es []entity.ProviderTool) []view.ProviderTool`
    - Example: `func FromProviderToolModelToProviderToolEntity(m model.ProviderTool) entity.ProviderTool`

### Entities (`internal/data/entity/`)
Entities are standard Go structs that specify the data passing between the controller layer and the handler/repository layers. Usually there's a `<Action><Entity>Request` and `<Action><Entity>Response` definition.

### Views (`internal/data/view/`)
Views define the external structures, like JSON inputs/outputs.
- Structs use `json:"fieldName"` struct tags for serialization properties.
- Do not contain behavior/business logic or persistence details here.

## 3. General Best Practices
- **ID parsing:** Use `monoflake.IDFromBase62(c.Params("id")).Int64()` to parse base-62 ID strings from route parameters to standard int64 IDs.
- **Param validation:** Always check for missing route parameters inside `FromHTTPRequestTo...RequestEntity` and return `nil` if validation fails.
- **Payload transformation:** Return raw `[]byte` payloads from `...ToHTTPResponse` mappers using `json.Marshal` internally, preventing the caller (handler) from doing repetitive marshaling.
- **Transport:** RESTFul API best practices for routes and input/ouput views.

## 4. Backend Service Testing & Mocking Design

This section outlines the design choices and standards for unit testing and mocking within the backend services.

### Project Structure for Mocks

We follow a strict isolation pattern for mocks to prevent circular dependencies and maintain a clean project structure.

- **Mock Placement**: Mocks are stored in a `mocks/` sub-package within each service directory.
  - Example: `internal/service/idgen/mocks/service.go`
- **Automation**: Every service interface includes a `//go:generate mockgen` directive to automate mock regeneration.
  ```go
  //go:generate mockgen -source=service.go -destination=mocks/service.go -package=mocks
  ```

### Tooling

- **Mocking Framework**: [GoMock](https://github.com/golang/mock) is our primary mocking tool. it provides type-safe mocks and expressive expectation setting.
- **Assertion Library**: [Testify/assert](https://github.com/stretchr/testify) for readable and concise test assertions.
- **Coverage**: Use standard Go toolchain for coverage analysis.
  ```bash
  go test -coverprofile=coverage.out ./internal/service/...
  go tool cover -html=coverage.out -o coverage.html
  ```

### Mocking Best Practices

#### Dependency Injection
Services should depend on interfaces (e.g., `config.Service`, `authentication.Service`) rather than concrete implementations. This allows for seamless mock injection during tests.

#### Config Mocking
Since most services use `Populate(key string, target any)` for configuration, we standardized on mocking this call to inject test-specific configuration values:

```go
mockConfig.EXPECT().Populate("serviceKey", gomock.Any()).DoAndReturn(func(key string, target any) error {
    t := target.(*serviceCfg)
    t.SomeField = "testValue"
    return nil
})
```

#### HTTP Client Mocking
For services that interact with external APIs (like `account` or `authorization`), we mock the `httpc.Service` interface or use `httptest` servers to intercept calls and return predictable JSON responses.

#### Running Tests
To run all service tests:
```bash
go test -v ./internal/service/...
```
