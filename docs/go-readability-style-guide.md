# Go readability style guide

**Edition:** 1.0 · **Verified:** 13 September 2026 · **Target:** Go 1.27.1

Go 1.27.1, released on 1 September 2026, is the latest stable release listed at verification time. This guide targets Go 1.27 language features and the Go 1.27.1 standard library. Recheck the [official release history](https://go.dev/doc/devel/release) before adopting a later patch.

This is a proposed team style standard for writing and reviewing readable Go. It combines established Go conventions with explicit team preferences and original examples. The preferences are recommendations for this guide, not claims that the Go project mandates one style for every codebase.

The objective is that a reader can answer: **What does this code do, what can fail, what can change, and who owns the work and resources?** Fewer lines are useful only when they make those answers easier to see.

## How to use this guide

- **MUST** identifies a correctness, contract, or adopted formatting requirement.
- **SHOULD** identifies the default readability preference. Keep an exception when the surrounding code or a documented constraint makes it clearer.
- **MAY** identifies an optional technique, not an adoption requirement.
- Each **GOR-001–GOR-060** rule has a bad and good example. “Bad” can mean incorrect or merely less readable in the stated context; the explanation distinguishes them.
- Snippets are independent alternatives. Imports, enclosing functions, and unrelated domain declarations are omitted where they would obscure the point. They are not one compilable source file. Placeholder domain calls such as `store.Save` represent application code, not standard library APIs.
- Public APIs, persisted data, error contracts, cancellation, ordering, and side effects take priority over cosmetic consistency. Signature changes shown in examples require a planned migration when applied to an existing public API.

### Navigation

| Rules | Area |
| --- | --- |
| GOR-001–006 | Formatting, names, documentation, locality |
| GOR-007–012 | Conditions, `switch`, loops, straightforward data flow |
| GOR-013–016 | Functions, parameters, returns, shadowing |
| GOR-017–027 | Types, APIs, dependencies, generics |
| GOR-028–035 | Errors, logging, resource ownership |
| GOR-036–041 | Context and concurrency |
| GOR-042–052 | Collections, modern APIs, strings, units, I/O |
| GOR-053–057 | Readable tests and benchmarks |
| GOR-058–060 | Version policy, tooling, review discipline |

### Version-sensitive features used here

| Feature | Available since | Readability use |
| --- | --- | --- |
| Generics and `any` | Go 1.18 | Type-safe reusable algorithms; GOR-024–026 |
| `errors.Join` | Go 1.20 | Preserve independent failures; GOR-034 |
| `slices`, `maps`, `log/slog`, `min`, `max`, `clear` | Go 1.21 | Recognizable standard operations; GOR-033, 044–046 |
| Integer `range`; per-iteration variables declared by loops | Go 1.22 | Direct counted loops; GOR-011, 039 |
| Range over iterator functions; `iter`; `maps.Keys`; `slices.Sorted` | Go 1.23 | Collection traversal; GOR-045, 047 |
| `testing.T.Context`; `testing.B.Loop` | Go 1.24 | Test lifetime and benchmark setup; GOR-055–057 |
| `sync.WaitGroup.Go`; stable `testing/synctest` | Go 1.25 | Visible task lifetime and deterministic tests; GOR-039, 055 |
| `new(expression)`; `errors.AsType`; revamped `go fix` | Go 1.26 | Remove incidental scaffolding; GOR-030, 050, 059 |
| Generic methods; `encoding/json/v2` in the standard release | Go 1.27 | Type-local APIs and intentional JSON policy; GOR-027, 051 |

Version references: [Go 1.22](https://go.dev/doc/go1.22), [Go 1.23](https://go.dev/doc/go1.23), [Go 1.24](https://go.dev/doc/go1.24), [Go 1.25](https://go.dev/doc/go1.25), [Go 1.26](https://go.dev/doc/go1.26), and [Go 1.27](https://go.dev/doc/go1.27). The `go` directive records the module's minimum supported Go version; the installed toolchain alone does not make every new feature appropriate for that module.

## Formatting, names, and documentation

### GOR-001 — Let `gofmt` own formatting

**MUST.** Format Go source with `gofmt`. Do not maintain manual alignment or compress several statements onto one line. Use blank lines for distinct steps. Break long expressions by introducing useful names, not an arbitrary hard line-length limit. [gofmt documentation](https://pkg.go.dev/cmd/gofmt)

**Bad — manual compression**

```go
func total(a,b int)int{r:=a+b;return r}
```

**Good**

```go
func total(a, b int) int {
	return a + b
}
```

**Boundary:** Formatting is mechanical. It does not establish whether names, API design, or error handling are readable.

### GOR-002 — Keep import origins visible

**SHOULD.** Put standard library imports first, separated from external imports. Avoid dot imports. Alias an import for a collision or meaningful disambiguation. Adopt a separate internal-import group only if the project needs it. [Go import guidance](https://go.dev/wiki/CodeReviewComments#imports)

**Bad — the call hides its package**

```go
import . "strings"

name := TrimSpace(input)
```

**Good**

```go
import "strings"

name := strings.TrimSpace(input)
```

**Boundary:** Side-effect imports, such as a database driver registration, need a deliberate purpose; keep that wiring near application startup or the relevant test.

### GOR-003 — Name the concept and use consistent initialisms

**SHOULD.** Use `customerID`, `HTTPClient`, and `requestURL`, not `customerId`, `HttpClient`, or `requestUrl`. Longer-lived values need more descriptive names. Conventional local names such as `i`, `n`, `err`, `ctx`, `r`, and `w` are useful when their role is obvious. Receiver names should be short and consistent, such as `s` for `Service`. [Go naming guidance](https://go.dev/wiki/CodeReviewComments#initialisms)

**Bad**

```go
func (this *Service) Do(u string) error {
	return this.store.Delete(u)
}
```

**Good**

```go
func (s *Service) DeleteCustomer(customerID string) error {
	return s.store.Delete(customerID)
}
```

**Boundary:** Do not expand familiar two-line loop variables into sentences. Name booleans so conditions read naturally: `enabled`, `hasAccess`, `canRetry`.

### GOR-004 — Make the package and exported name read together

**SHOULD.** Choose cohesive, lowercase package names. Read names from a caller's perspective: `payment.Service`, `payment.New`, `payment.ParseStatus`. Avoid dumping unrelated behavior into `util`, `common`, or `manager`. Organize packages around responsibilities, not a Java-style class hierarchy. [Package names](https://go.dev/blog/package-names)

**Bad — redundant package stutter**

```go
package payment

type PaymentService struct{}
```

**Good**

```go
package payment

type Service struct{}
```

**Boundary:** `NewService` can be clearer than `New` when a package has several independently constructed types. Do not split cohesive code into many packages merely to shorten files.

### GOR-005 — Document observable contracts and non-obvious reasons

**SHOULD.** Document exported declarations with sentences beginning with the declared name. Explain absence, mutation, ownership, ordering, cancellation, and concurrency safety when relevant. Comments should explain constraints or decisions rather than narrate syntax. Add runnable examples for APIs with a non-obvious call sequence. [Go doc comments](https://go.dev/doc/comment)

**Bad**

```go
// Gets orders.
func (s *Store) Orders() []Order {
	return slices.Clone(s.orders)
}
```

**Good**

```go
// Orders returns a shallow copy in insertion order.
// Callers may replace slice elements; referenced objects remain shared.
// Orders must not run concurrently with store mutation.
func (s *Store) Orders() []Order {
	return slices.Clone(s.orders)
}
```

**Boundary:** Do not add boilerplate comments to every obvious local assignment. Update contract comments when behavior changes.

### GOR-006 — Declare values near their use

**SHOULD.** Give each phase its own local values. Avoid a function-wide declaration block used by unrelated operations. Use `:=` when the initializer explains the type; use `var` when a zero value is the intended starting state.

**Bad — unrelated declarations are separated from their purpose**

```go
var customer Customer
var invoice Invoice
var err error
customer, err = loadCustomer(id)
if err != nil {
	return err
}
invoice = buildInvoice(customer)
return saveInvoice(invoice)
```

**Good**

```go
customer, err := loadCustomer(id)
if err != nil {
	return err
}

invoice := buildInvoice(customer)
return saveInvoice(invoice)
```

**Boundary:** Group declarations that form one concept, such as related constants or a small test table.

## Conditions and control flow

### GOR-007 — Use guard clauses to keep the main path visible

**SHOULD.** Reject invalid input and handle failures early. Remove an `else` when the preceding branch returns. Preserve the original validation order and error contract.

**Bad**

```go
func submit(order *Order) error {
	if order != nil {
		if order.TotalMinor > 0 {
			return save(order)
		} else {
			return ErrInvalidTotal
		}
	} else {
		return ErrMissingOrder
	}
}
```

**Good**

```go
func submit(order *Order) error {
	if order == nil {
		return ErrMissingOrder
	}
	if order.TotalMinor <= 0 {
		return ErrInvalidTotal
	}
	return save(order)
}
```

**Boundary:** Early returns are not a quota. Keep a balanced binary `if/else` when both branches naturally contribute to the next step.

### GOR-008 — Prefer `switch` over multi-branch `if/else if`

**SHOULD.** Use a value `switch` when choosing among several alternatives for one value. Use an expressionless `switch` for an ordered set of related conditions. Keep `if` for a simple binary decision. This is an explicit readability preference, not a language requirement. [Effective Go: control structures](https://go.dev/doc/effective_go#control-structures)

**Bad — repeated comparisons obscure the alternatives**

```go
if status == StatusPending {
	return "pending"
} else if status == StatusPaid {
	return "paid"
} else if status == StatusRefunded {
	return "refunded"
} else {
	return "unknown"
}
```

**Good**

```go
switch status {
case StatusPending:
	return "pending"
case StatusPaid:
	return "paid"
case StatusRefunded:
	return "refunded"
default:
	return "unknown"
}
```

**Keep simple binary decisions simple:**

```go
if enabled {
	start()
} else {
	stop()
}
```

**For ordered related conditions, use an expressionless switch:**

```go
switch {
case attempts == 0:
	return PhaseInitial
case attempts < limit:
	return PhaseRetry
default:
	return PhaseExhausted
}
```

**Boundary:** Separate independent conditions remain separate `if` statements. Converting them into a `switch` would make them mutually exclusive and change behavior. Preserve condition evaluation order and side effects during conversion.

### GOR-009 — Name difficult predicates and avoid double negatives

**SHOULD.** Introduce a boolean or helper that communicates a domain decision. Keep short conditions inline. Preserve short-circuit evaluation when a later expression depends on an earlier check.

**Bad**

```go
if !customer.Disabled && !(invoice.TotalMinor <= 0) && !invoice.Paid {
	return charge(invoice)
}
```

**Good**

```go
canCharge := !customer.Disabled && invoice.TotalMinor > 0 && !invoice.Paid
if canCharge {
	return charge(invoice)
}
```

**Boundary:** Do not precompute `customer.Active` before checking whether `customer` is nil. Avoid helpers that merely rename one obvious operator.

### GOR-010 — Make switch cases and unknown values explicit

**SHOULD.** Combine cases that share behavior. Avoid `fallthrough` unless executing the next case body is the deliberate meaning. Handle invalid domain values explicitly where they can arrive from external input; a `switch` is not an exhaustive enum check. [Go specification: switch statements](https://go.dev/ref/spec#Switch_statements)

**Bad — unnecessarily couples neighboring cases**

```go
switch status {
case StatusPending:
	fallthrough
case StatusQueued:
	return true
default:
	return false
}
```

**Good**

```go
switch status {
case StatusPending, StatusQueued:
	return true
default:
	return false
}
```

**Boundary:** A type switch is useful for genuine heterogeneous input. Do not replace a clear interface method with a central type switch that must know every implementation.

### GOR-011 — Choose the loop form that shows its purpose

**SHOULD.** Range over values when reading them; range over indices when replacing slice elements. Use `for range n` for a simple count with no index use. Keep a three-clause loop when its index progression matters.

**Bad — mutates a copy of each struct**

```go
for _, order := range orders {
	order.Processed = true
}
```

**Good — mutates the actual elements of `[]Order`**

```go
for i := range orders {
	orders[i].Processed = true
}
```

**Boundary:** For `[]*Order`, the ranged pointer still refers to the object. In Go 1.22+, loop variables declared by the loop are distinct per iteration; assignment into a variable declared outside the loop still reuses that variable. [Go 1.22 loop changes](https://go.dev/doc/go1.22#language)

### GOR-012 — Prefer direct loops when transformation chains obscure work

**SHOULD.** Write a direct loop when filtering, mapping, and accumulating together gives the clearest view of conditions, errors, allocation, or early exit. Keep small pure standard operations and short established pipelines when they read more clearly.

**Bad — illustrative project helpers add indirection to a simple operation**

```go
ids := collect(mapEach(filter(orders, func(o Order) bool {
	return o.Status == StatusPaid
}), func(o Order) string {
	return o.ID
}))
```

**Good**

```go
var ids []string
for _, order := range orders {
	if order.Status != StatusPaid {
		continue
	}
	ids = append(ids, order.ID)
}
```

**Boundary:** Assume the helper version preserves order and returns nil when no item matches; verify this when refactoring. `filter`, `mapEach`, and `collect` above are not Go built-ins. Do not replace readable `slices.Contains` or `strings.Join` with manual loops.

## Functions and parameters

### GOR-013 — Keep each function at one useful level of detail

**SHOULD.** Extract a function when it names a meaningful operation, isolates a policy, or separates orchestration from mechanics. Avoid fixed function-length rules and one-line wrappers that make readers jump between files without learning anything.

**Bad — a workflow exposes string assembly details**

```go
func welcome(customer Customer) error {
	body := "Hello " + customer.Name + ",\n"
	body += "Your account is ready.\n"
	body += "Customer reference: " + customer.ID
	return sendMail(customer.Email, body)
}
```

**Good**

```go
func welcome(customer Customer) error {
	return sendMail(customer.Email, welcomeBody(customer))
}

func welcomeBody(customer Customer) string {
	return fmt.Sprintf(
		"Hello %s,\nYour account is ready.\nCustomer reference: %s",
		customer.Name,
		customer.ID,
	)
}
```

**Boundary:** Extraction is useful here because the message is a separately reviewed template. For a trivial one-off message, keeping it inline can be clearer. Preserve sequencing when extracting I/O or transaction code.

### GOR-014 — Make call-site arguments explain themselves

**SHOULD.** Replace ambiguous boolean combinations or long lists of same-typed arguments with named options or domain types. Put `context.Context` first and errors last in results. Do not create a configuration framework for a two-argument function.

**Bad**

```go
report, err := buildReport(ctx, customerID, true, false, 30)
```

**Good**

```go
report, err := buildReport(ctx, customerID, ReportOptions{
	IncludeArchived: true,
	IncludeDrafts:   false,
	Lookback:        30 * 24 * time.Hour,
})
```

**Boundary:** The example defines an elapsed 30-day window. Calendar-month rules need calendar arithmetic. Functional options are appropriate for genuinely evolving optional configuration; a struct is often easier to scan.

### GOR-015 — Return values explicitly; name results when that adds meaning

**SHOULD.** Prefer explicit return expressions. Name results when names document their meaning or a deferred function must update them. Avoid naked returns that force the reader to reconstruct earlier assignments.

**Bad**

```go
func discount(total int64) (amount int64) {
	amount = total / 10
	return
}
```

**Good**

```go
func discount(total int64) int64 {
	return total / 10
}
```

**Boundary:** `(start, end int)` may document two same-typed results well. A named `err` modified by deferred cleanup is legitimate; GOR-034 shows that case. Do not combine independent failures by silently returning only one.

### GOR-016 — Avoid shadowing that changes which value is returned

**MUST** preserve the intended variable. **SHOULD** use distinctive names when a nested declaration would make the data flow ambiguous. A scoped `if err := operation(); err != nil` is useful when the error is handled entirely there.

**Bad — the inner result never updates the returned variable**

```go
func readConfig(useCache bool) ([]byte, error) {
	var data []byte
	if useCache {
		data, err := loadCachedConfig()
		if err != nil {
			return nil, err
		}
		validate(data)
	}
	return data, nil
}
```

**Good — keep the complete branch local**

```go
func readConfig(useCache bool) ([]byte, error) {
	if !useCache {
		return nil, nil
	}
	data, err := loadCachedConfig()
	if err != nil {
		return nil, err
	}
	validate(data) // Assumed not to return an error in this example.
	return data, nil
}
```

**Boundary:** This pair fixes a bug rather than preserving the bad code's output. Do not prohibit every shadowed `err`; focus on confusing ownership and missed assignments.

## Types, APIs, and dependencies

### GOR-017 — Use helpful zero values and constructors for real invariants

**SHOULD.** Allow a type's zero value to work when natural. Use constructors when required dependencies or validated configuration make a zero value invalid. Keep required initialization visible.

**Bad — ceremony for a type with a usable zero value**

```go
buffer := new(bytes.Buffer)
buffer.Reset()
buffer.WriteString("ready")
```

**Good**

```go
var buffer bytes.Buffer
buffer.WriteString("ready")
```

**Boundary:** `bytes.Buffer.WriteString` always returns a nil error, so this deliberate omission is justified by its contract. A service requiring a database should have an explicit constructor, not a hidden first-use connection. [bytes.Buffer](https://pkg.go.dev/bytes#Buffer)

### GOR-018 — Represent domain alternatives with named types and constants

**SHOULD.** Use typed constants for meaningful domain states. Reserve an invalid zero state when accidental omission must be detected. Parse and validate external values at the boundary. [Go constants](https://go.dev/blog/constants)

**Bad**

```go
if order.Status == 2 {
	return releaseShipment(order)
}
```

**Good**

```go
type Status uint8

const (
	StatusUnknown Status = iota
	StatusPending
	StatusPaid
)

if order.Status == StatusPaid {
	return releaseShipment(order)
}
```

**Boundary:** Named types do not create closed enums. For persisted or wire values, assign stable explicit numbers or strings rather than letting a future `iota` insertion renumber them. A valid zero state is fine when deliberate.

### GOR-019 — Use keyed struct literals for meaningful fields

**SHOULD.** Show field names when a literal has multiple values whose positions are not self-evident, especially booleans and values of the same type.

**Bad**

```go
customer := Customer{"c-42", "Ada", true}
```

**Good**

```go
customer := Customer{
	ID:     "c-42",
	Name:   "Ada",
	Active: true,
}
```

**Boundary:** Go 1.27 permits field-selector keys for embedded fields. Use them only if the resulting construction remains obvious; nested literals are still valid. Never use a style edit to overwrite intentionally omitted defaults. [Go 1.27 language changes](https://go.dev/doc/go1.27#language)

### GOR-020 — Choose receivers by mutation and copying semantics

**MUST.** Do not copy a mutex or other non-copyable state after use. **SHOULD.** Use pointer receivers for mutating or identity-bearing types; use value receivers for small independent values. Keep receiver choices consistent unless the distinction is intentional and documented.

**Bad — copies the lock and shares the underlying map**

```go
func (c Cache) Put(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}
```

**Good**

```go
func (c *Cache) Put(key string, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[string]string)
	}
	c.values[key] = value
}
```

**Boundary:** `Cache` contains `mu sync.Mutex` and `values map[string]string`; all access must use the same lock. A value receiver can still mutate referenced data, so it does not imply deep immutability. [sync contracts](https://pkg.go.dev/sync)

### GOR-021 — Define small interfaces around what the consumer needs

**SHOULD.** Use an interface when a consumer needs a behavioral boundary. Keep it near the consumer. Usually return a concrete type from a constructor so callers can see the real capabilities. Reuse suitable standard interfaces such as `io.Reader`.

**Bad — billing depends on unrelated customer operations**

```go
type CustomerRepository interface {
	Find(context.Context, string) (Customer, error)
	Save(context.Context, Customer) error
	Delete(context.Context, string) error
	Reindex(context.Context) error
}

type BillingService struct {
	customers CustomerRepository
}
```

**Good**

```go
type CustomerFinder interface {
	Find(context.Context, string) (Customer, error)
}

type BillingService struct {
	customers CustomerFinder
}
```

**Boundary:** Do not generate an interface for every struct or export test-only abstractions. Returning an interface is valid when hiding the implementation is the API's purpose. [Go interface guidance](https://go.dev/wiki/CodeReviewComments#interfaces)

### GOR-022 — Use embedding only when promotion is part of the API

**SHOULD.** Prefer named fields when a dependency is an implementation detail. Embedding promotes methods and can unintentionally expand the public API. It is composition, not class inheritance. [Effective Go: embedding](https://go.dev/doc/effective_go#embedding)

**Bad — exposes the database's methods through the service**

```go
type Service struct {
	*sql.DB
}
```

**Good**

```go
type Service struct {
	db *sql.DB
}
```

**Boundary:** Embedding an interface in a larger capability interface can be clear. Changing an existing exported embedding is an API change, so plan its callers' migration.

### GOR-023 — Make dependencies and startup work explicit

**SHOULD.** Inject dependencies through constructors or parameters. Keep network access, configuration loading, and background startup out of `init`. Package-level immutable lookup data and deliberate registration are reasonable exceptions.

**Bad — initialization order hides an external dependency**

```go
var store *Store

func init() {
	store = mustOpenStore(os.Getenv("DATABASE_URL"))
}
```

**Good**

```go
type Service struct {
	store *Store
}

func NewService(store *Store) (*Service, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	return &Service{store: store}, nil
}
```

**Boundary:** The application opens and closes the store at its composition point. Avoid dependency containers that resolve services by string and move missing-dependency errors to runtime.

### GOR-024 — Prefer domain types over `any` and reflection

**SHOULD.** Represent known data with structs, named types, and ordinary parameters. `any` is an alias for `interface{}`, not a type-safe container. Use it at genuinely dynamic boundaries. [Go FAQ](https://go.dev/doc/faq)

**Bad — runtime assertions reconstruct a known schema**

```go
func customerID(record map[string]any) string {
	return record["customer_id"].(string)
}
```

**Good**

```go
type PaymentRequest struct {
	CustomerID  string
	AmountMinor int64
}

func customerID(request PaymentRequest) string {
	return request.CustomerID
}
```

**Boundary:** The small accessor isolates the contrast; normal callers can read `request.CustomerID` directly. Keep dynamic validation explicit when working with unknown JSON schemas, plugins, or reflection-based infrastructure.

### GOR-025 — Use generics for a real shared algorithm

**SHOULD.** Introduce type parameters when the algorithm stays the same across types and static types remove duplication or assertions. Keep domain-specific functions concrete. [When to use generics](https://go.dev/blog/when-generics)

**Bad — loses the element type**

```go
func first(values []any) (any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	return values[0], true
}
```

**Good**

```go
func first[T any](values []T) (T, bool) {
	if len(values) == 0 {
		var zero T
		return zero, false
	}
	return values[0], true
}
```

**Boundary:** `[]Customer` is not assignable to `[]any`; the bad signature also makes callers convert data. Do not introduce a generic repository or collection framework before shared requirements justify it.

### GOR-026 — Keep constraints and type arguments readable

**SHOULD.** Constrain only operations the implementation requires. Let inference omit obvious type arguments. Give a complex constraint a meaningful name when reused; avoid a giant union of unrelated domain types.

**Bad — repeats types already determined by the argument**

```go
type CustomerIDs []string

ids := CustomerIDs{"c-42", "c-91"}
found := slices.Contains[CustomerIDs, string](ids, "c-42")
```

**Good**

```go
type CustomerIDs []string

ids := CustomerIDs{"c-42", "c-91"}
found := slices.Contains(ids, "c-42")
```

**Boundary:** Explicit type arguments are useful when inference cannot determine an output type or when the choice is important at the call site. `comparable` permits equality; it does not define domain equivalence. [slices API](https://pkg.go.dev/slices#Contains)

### GOR-027 — Use Go 1.27 generic methods when they clarify a type's API

**SHOULD.** A generic method can group a type-dependent operation beside the state it uses. A generic free function remains appropriate for independent algorithms. Do not force chains of transformations merely because methods now support their own type parameters. Generic interface methods are not supported. [Generic methods](https://go.dev/blog/generic-methods)

**Bad — known caller types are erased inside a formatter**

```go
type Formatter struct {
	Prefix string
}

func (f Formatter) Format(value any, render func(any) string) string {
	return f.Prefix + render(value)
}
```

**Good — Go 1.27+**

```go
type Formatter struct {
	Prefix string
}

func (f Formatter) Format[T any](value T, render func(T) string) string {
	return f.Prefix + render(value)
}
```

**Boundary:** This changes the API and requires a Go 1.27 language baseline. For earlier supported Go versions, use a generic free function taking `Formatter`. If consumers need an interface, expose an ordinary non-generic method with a suitable fixed signature; a generic method cannot implement an interface method.

## Errors, logging, and resources

### GOR-028 — Handle errors where their meaning is still clear

**MUST.** Check fallible operations before using their result. Return the error, recover deliberately, or handle it completely. Do not use `_` to hide an unexplained failure. Keep ordinary error strings lowercase and without trailing punctuation, except proper names or acronyms. [Go error guidance](https://go.dev/wiki/CodeReviewComments#error-strings)

**Bad**

```go
count, _ := strconv.Atoi(input)
return allocate(count)
```

**Good**

```go
count, err := strconv.Atoi(input)
if err != nil {
	return fmt.Errorf("parse item count: %w", err)
}
return allocate(count)
```

**Boundary:** A documented API guarantee can justify ignoring an error, as in GOR-017. Cleanup errors need their own policy; “always ignore `Close`” is not a valid general rule.

### GOR-029 — Add useful error context and preserve intended causes

**SHOULD.** Add the operation and safe identifying context at a meaningful boundary. Use `%w` when callers should inspect the underlying cause. Treat exposed causes as part of the API; translate storage-specific failures when they should remain private. [Wrapping error contracts](https://go.dev/blog/go1.13-errors)

**Bad — destroys cause inspection**

```go
return fmt.Errorf("save invoice %s failed: %v", invoice.ID, err)
```

**Good — where the repository cause is intentionally exposed**

```go
return fmt.Errorf("save invoice %s: %w", invoice.ID, err)
```

**Boundary:** Do not wrap at every pass-through layer with repetitive text. Do not wrap nil and return a non-nil error accidentally. Avoid putting secrets or entire payloads into error messages.

### GOR-030 — Inspect errors with `errors.Is` and typed matching

**MUST** avoid error-string matching for program decisions when a structured contract exists. **SHOULD** use `errors.Is` for a sentinel and `errors.AsType` for a typed cause on this baseline. Both work through wrapping. [errors package](https://pkg.go.dev/errors)

**Bad**

```go
if err != nil && err.Error() == "payment declined" {
	return Declined, nil
}
```

**Good**

```go
if errors.Is(err, ErrPaymentDeclined) {
	return Declined, nil
}
```

**Typed cause — Go 1.26+:**

```go
if decline, ok := errors.AsType[*DeclineError](err); ok {
	return decline.Code
}
```

**Boundary:** `DeclineError` must implement `error`. `errors.As(err, &target)` remains valid for older baselines and special interface targets; do not erase that distinction. An assertion such as `err.(*DeclineError)` sees only the outer error.

### GOR-031 — Reserve panic for violated program invariants

**SHOULD.** Return errors for invalid input and expected external failures. Keep `panic` for an unrecoverable programmer error or a clearly documented `Must...` contract. Recover only at a deliberate boundary with an explicit policy. [Effective Go: panic and recover](https://go.dev/doc/effective_go#panic)

**Bad — user input can terminate an unprotected call path**

```go
func parseLimit(input string) int {
	limit, err := strconv.Atoi(input)
	if err != nil {
		panic(err)
	}
	return limit
}
```

**Good**

```go
func parseLimit(input string) (int, error) {
	limit, err := strconv.Atoi(input)
	if err != nil {
		return 0, fmt.Errorf("parse limit: %w", err)
	}
	return limit, nil
}
```

**Boundary:** Validate the allowed range separately. `recover` must run in a deferred function on the panicking goroutine; a parent goroutine cannot use it to catch a child's panic. Do not convert panics into silent success.

### GOR-032 — Return a genuinely nil error on success

**MUST.** Do not return a typed nil pointer as an `error` and expect the interface to be nil. The same distinction applies to other interface values. [Go FAQ: nil errors](https://go.dev/doc/faq#nil_error)

**Bad — `err != nil` at the caller**

```go
func validate() error {
	var validationError *ValidationError
	return validationError
}
```

**Good**

```go
func validate() error {
	return nil
}
```

**Boundary:** Assume `*ValidationError` implements `error`. An interface holds both a dynamic type and value. Return typed errors only when an actual failure exists.

### GOR-033 — Log where an operation is owned; use structured fields

**SHOULD.** Let lower layers return useful errors and let the operation boundary decide how to report failure. Use stable event messages and fields. Avoid repeating the same failure at every layer. [Structured logging with slog](https://pkg.go.dev/log/slog)

**Bad — a lower layer both logs and passes the failure upward**

```go
if err := store.Save(ctx, invoice); err != nil {
	logger.ErrorContext(ctx, "save failed", "error", err)
	return err
}
```

**Good — boundary owns the final report**

```go
if err := service.Submit(ctx, invoice); err != nil {
	logger.ErrorContext(ctx, "invoice submission failed",
		"invoice_id", invoice.ID,
		"error", err,
	)
	return failureResponse(err)
}
```

**Boundary:** `failureResponse` represents the application's response mapping. A retry attempt can merit a separate debug event. Returning or panicking with an error does not by itself define a logging destination; handlers and application configuration do.

### GOR-034 — Register cleanup immediately and preserve meaningful close errors

**MUST.** Make resource ownership clear. Register cleanup after successful acquisition or accepting ownership. For output operations, a failed `Close` can indicate failure even when writing succeeded. [io.Closer](https://pkg.go.dev/io#Closer) and [errors.Join](https://pkg.go.dev/errors#Join)

**Bad — an early write error skips closing**

```go
func writeAndClose(w io.WriteCloser, data []byte) error {
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return err
	}
	return w.Close()
}
```

**Good**

```go
// writeAndClose takes ownership of w and closes it exactly once.
func writeAndClose(w io.WriteCloser, data []byte) (err error) {
	defer func() {
		if closeErr := w.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close output: %w", closeErr))
		}
	}()

	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
```

**Boundary:** The named result lets the deferred function add a close failure. Do not close a caller-owned reader or writer unless the contract transfers ownership. `defer` arguments are evaluated when registered; use a closure when a value must be evaluated at function exit. Process exit via `os.Exit` does not run defers.

### GOR-035 — Match cleanup scope to each loop iteration

**SHOULD.** Avoid accumulating deferred closes across a long loop. Move one resource's complete work into a helper or use an API that owns the resource lifecycle. [os.ReadFile](https://pkg.go.dev/os#ReadFile)

**Bad — files stay open until the surrounding function returns**

```go
for _, path := range paths {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := processSmallFile(file); err != nil {
		return err
	}
}
```

**Good — for files intentionally bounded to a small size**

```go
for _, path := range paths {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read input %q: %w", path, err)
	}
	if err := processSmallFile(bytes.NewReader(data)); err != nil {
		return err
	}
}
```

**Boundary:** Whole-file reading changes memory use and read timing. For large or unbounded files, use a helper that opens, streams, and closes one file per call with the appropriate close-error policy. Do not call a whole-file conversion a behavior-neutral style change without checking these constraints.

## Context and concurrency

### GOR-036 — Propagate context and release derived contexts

**MUST.** Preserve the caller's deadline and cancellation for request-scoped work. Pass `ctx` first. Call the cancel function for a derived context. Do not replace a request's context with `context.Background()` halfway through the call chain. [context documentation](https://pkg.go.dev/context)

**Bad**

```go
func (s *Service) Load(ctx context.Context, id string) (Customer, error) {
	return s.store.Find(context.Background(), id)
}
```

**Good**

```go
func (s *Service) Load(ctx context.Context, id string) (Customer, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.store.Find(lookupCtx, id)
}
```

**Boundary:** The timeout is an illustrative service policy; use configuration when appropriate. Pure calculations need no context. Store long-lived dependencies in structs, and pass request contexts to methods. Detached background work needs an explicit owner and shutdown policy.

### GOR-037 — Keep business inputs and dependencies out of context values

**SHOULD.** Use ordinary parameters for required inputs and fields for dependencies. Context values suit request-scoped metadata crossing API boundaries. If used, choose private typed keys and accessors instead of collision-prone string keys. [Context values](https://pkg.go.dev/context#WithValue)

**Bad**

```go
ctx = context.WithValue(ctx, "invoice", invoice)
return submit(ctx)
```

**Good**

```go
return submit(ctx, invoice)
```

**Boundary:** A trace identifier is metadata; an invoice to process is a business argument. Context is not a service locator or an untyped optional-argument bag.

### GOR-038 — Make goroutine ownership and completion visible

**MUST.** Each goroutine needs a lifetime, an owner, and a way to observe relevant failures. Start concurrency only when it serves a requirement. Prefer a synchronous call when the caller immediately needs completion.

**Bad — reports success before the work finishes and drops errors**

```go
func submit(ctx context.Context, invoice Invoice) error {
	go store.Save(ctx, invoice)
	return nil
}
```

**Good — synchronous contract**

```go
func submit(ctx context.Context, invoice Invoice) error {
	return store.Save(ctx, invoice)
}
```

**Boundary:** A real asynchronous API should define acceptance, durable queuing if required, eventual failure reporting, cancellation, and shutdown. Merely buffering an error channel does not ensure someone observes the error. [Go pipelines and cancellation](https://go.dev/blog/pipelines)

### GOR-039 — Use `WaitGroup.Go` for simple task tracking and bound fan-out

**SHOULD.** On Go 1.25+, `WaitGroup.Go` simplifies independent tasks that do not return errors and are guaranteed not to panic. Limit concurrency when input size is unbounded. For related fallible tasks, consider an error-aware group with cancellation and a limit.

**Bad — `Wait` can run before the goroutine registers itself**

```go
var wg sync.WaitGroup
for _, index := range indexes {
	go func() {
		wg.Add(1)
		defer wg.Done()
		index.RefreshLocal()
	}()
}
wg.Wait()
```

**Good — assume each refresh is independent and cannot panic**

```go
var wg sync.WaitGroup
for _, index := range indexes { // A small, bounded set.
	wg.Go(func() {
		index.RefreshLocal()
	})
}
wg.Wait()
```

**Boundary:** `WaitGroup.Go` does not propagate errors, limit work, cancel tasks, or recover panics. Manual `Add` before `go` with deferred `Done` remains valid. The loop-declared `index` has per-iteration scope on this baseline; no `index := index` workaround is needed. [WaitGroup.Go](https://pkg.go.dev/sync#WaitGroup.Go)

### GOR-040 — Define channel ownership and cancellation at blocking points

**MUST.** Ensure blocked sends and receives can finish when a caller stops. The sender or coordinating owner closes a channel after all sends finish; receivers should not close a shared input. Directional channel parameters communicate intent. [Pipelines and cancellation](https://go.dev/blog/pipelines)

**Bad — can block forever when the consumer leaves**

```go
func sendResult(out chan Result, result Result) {
	out <- result
}
```

**Good**

```go
func sendResult(ctx context.Context, out chan<- Result, result Result) error {
	select {
	case out <- result:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

**Boundary:** If both cases are ready, either may be selected; cancellation is not automatically prioritized. This helper does not own channel closure. Avoid busy-loop `select` statements with a `default` when blocking is intended.

### GOR-041 — Keep locks close to the state they protect

**MUST.** Synchronize shared mutable state. **SHOULD.** Keep critical sections short and avoid holding locks across slow I/O. Document the protected fields. Use a mutex for ordinary shared state; channels and atomics are not automatically more readable. [Go memory model](https://go.dev/ref/mem)

**Bad — storage latency blocks unrelated cache access**

```go
c.mu.Lock()
defer c.mu.Unlock()
return persistSnapshot(ctx, c.values)
```

**Good — persistence accepts a point-in-time snapshot**

```go
c.mu.Lock()
snapshot := maps.Clone(c.values) // map[string]string
c.mu.Unlock()

return persistSnapshot(ctx, snapshot)
```

**Boundary:** This intentionally defines snapshot semantics; it is unsuitable when the operation must be atomic with later mutations. A shallow copy is sufficient here because values are strings. Every reader and writer of `c.values` must follow the same locking policy. Race-free individual calls do not guarantee a race-free compound operation.

## Collections, values, and I/O

### GOR-042 — Distinguish missing map entries from zero values

**MUST.** Use the comma-ok lookup when presence matters. Initialize a nil map before assignment. Do not turn “missing” into a business value accidentally. [Go maps](https://go.dev/blog/maps)

**Bad — a stored zero is confused with an absent customer**

```go
balance := balances[customerID]
if balance == 0 {
	return ErrCustomerNotFound
}
```

**Good**

```go
balance, ok := balances[customerID]
if !ok {
	return ErrCustomerNotFound
}
```

**Boundary:** Reading, ranging, deleting, or clearing a nil map is permitted; assigning an entry panics. Use `map[ID]struct{}` for a set when only membership matters, or `map[ID]bool` when the boolean itself has meaning.

### GOR-043 — Treat nil and empty as a contract decision

**SHOULD.** Use a nil slice as an ordinary empty accumulator when no contract distinguishes it. At serialization boundaries, deliberately choose the output representation. Avoid repeated `slice != nil` checks when only `len(slice)` matters.

**Bad — output for an empty result is accidental**

```go
// This endpoint promises {"items":[]} when no orders exist.
// Import: "encoding/json" (v1).
var items []Order
return json.Marshal(ListResponse{Items: items})
```

**Good — explicitly satisfies the stated v1 JSON contract**

```go
// Import: "encoding/json" (v1).
items := make([]Order, 0)
return json.Marshal(ListResponse{Items: items})
```

**Boundary:** Assume `Items []Order` has tag `json:"items"` without an omit option. JSON v1 encodes nil slices as `null`; JSON v2 normally encodes them as `[]`. Nil maps differ similarly. `omitempty`, `omitzero`, and other options need separate contract checks. [JSON v1](https://pkg.go.dev/encoding/json) and [JSON v2](https://pkg.go.dev/encoding/json/v2)

### GOR-044 — Make slice and map aliasing explicit

**MUST.** Do not mutate caller-owned data unexpectedly. **SHOULD.** Clone when the function promises an independent result, and document shallow versus deep copying. An assignment copies a slice descriptor or map reference, not all referenced data.

**Bad — changes the caller's element order**

```go
func sortedIDs(ids []string) []string {
	result := ids
	slices.Sort(result)
	return result
}
```

**Good**

```go
func sortedIDs(ids []string) []string {
	result := slices.Clone(ids)
	slices.Sort(result)
	return result
}
```

**Boundary:** `slices.Clone` and `maps.Clone` are shallow. Pointers and nested slices/maps remain shared. Appending can reuse a backing array when capacity permits. Do not claim a clone makes nested objects immutable or concurrency-safe. [Slice internals](https://go.dev/blog/slices-intro)

### GOR-045 — Sort map keys when output order matters

**MUST.** Make ordering explicit for reports, golden files, stable logs, and reproducible presentation. Do not rely on map iteration order. [maps.Keys](https://pkg.go.dev/maps#Keys)

**Bad — report order changes unpredictably**

```go
for id, balance := range balances {
	if err := writeRow(id, balance); err != nil {
		return err
	}
}
```

**Good — Go 1.23+**

```go
for _, id := range slices.Sorted(maps.Keys(balances)) {
	if err := writeRow(id, balances[id]); err != nil {
		return err
	}
}
```

**Boundary:** Sorting adds allocation and work. Omit it for a computation whose result and side effects are order-independent. Protect the map or snapshot it before concurrent access; sorting does not supply synchronization.

### GOR-046 — Prefer recognizable standard operations

**SHOULD.** Use a standard helper when it states the same operation directly: `slices.Contains`, `slices.SortFunc`, `maps.Clone`, `strings.Cut`, `strings.Join`, and built-ins such as `min`, `max`, and `clear`. Match semantics before replacing code.

**Bad — a custom loop for ordinary membership**

```go
found := false
for _, id := range ids {
	if id == requestedID {
		found = true
		break
	}
}
```

**Good**

```go
found := slices.Contains(ids, requestedID)
```

**Boundary:** `clear(slice)` zeroes existing elements without changing length; `clear(map)` removes entries while retaining the map. `min` and `max` are not domain validation. A comparison function must satisfy its API's ordering requirements. Avoid a new dependency merely to replace this one-line standard call. [Built-ins](https://pkg.go.dev/builtin) and [slices](https://pkg.go.dev/slices)

### GOR-047 — Keep iterator behavior and early termination obvious

**MUST.** An iterator producer must stop calling `yield` once it returns false. **SHOULD.** Use range syntax to consume iterators. Document ordering, reuse, mutation, and errors for an iterator API. [iter documentation](https://pkg.go.dev/iter)

**Bad — ignores the consumer's request to stop**

```go
func activeOrders(orders []Order) iter.Seq[Order] {
	return func(yield func(Order) bool) {
		for _, order := range orders {
			if order.Active {
				yield(order)
			}
		}
	}
}
```

**Good**

```go
func activeOrders(orders []Order) iter.Seq[Order] {
	return func(yield func(Order) bool) {
		for _, order := range orders {
			if !order.Active {
				continue
			}
			if !yield(order) {
				return
			}
		}
	}
}
```

**Boundary:** This iterator sees the captured slice; it does not take a snapshot. A one-off slice loop may need no iterator abstraction. Resource-backed iteration needs cleanup on completion and early exit, plus an explicit error-reporting contract. When using `iter.Pull`, arrange to call its `stop` function.

### GOR-048 — Put units and numeric meaning in types and names

**SHOULD.** Use `time.Duration` and `time.Time` for temporal values. Name raw counts with units. Represent money with an explicit currency and an agreed exact representation; use integer minor units where the domain supports them, or a suitable decimal representation for finer precision.

**Bad — units and rounding policy are hidden**

```go
timeout := 5000
amount := 19.99
```

**Good — this contract uses SEK minor units**

```go
timeout := 5 * time.Second
amount := Money{
	Currency: "SEK",
	Minor:    1999,
}
```

**Boundary:** Currency scale is not universally two digits. Define rounding and overflow behavior; do not convert a binary float into “exact” money after the fact. Elapsed durations and calendar periods are different: use `AddDate` for calendar arithmetic. Compare instants with `Time.Equal` when location and monotonic representation should not matter. [time documentation](https://pkg.go.dev/time)

### GOR-049 — Distinguish bytes, runes, and user-perceived characters

**MUST.** Choose string operations that match the requirement. `len(s)` counts bytes. Rune counts measure Unicode code points, not grapheme clusters. Prefer `strings.Join` for joining known parts and `strings.Builder` for incremental assembly when useful. [Strings, bytes, runes and characters](https://go.dev/blog/strings)

**Bad — the requirement is a code-point limit**

```go
if len(name) > maxRunes {
	return ErrNameTooLong
}
```

**Good**

```go
if utf8.RuneCountInString(name) > maxRunes {
	return ErrNameTooLong
}
```

**Boundary:** Sinhala combining sequences and many emoji can contain multiple code points in one perceived character. A visual-character limit needs a grapheme-aware policy. Byte limits remain correct for byte-bounded protocols. Simple `prefix + value` is readable; do not require builders everywhere.

### GOR-050 — Use `new(expression)` for clear optional scalar values

**SHOULD.** On Go 1.26+, prefer the built-in form over an immediately invoked closure or a generic pointer helper whose only purpose is constructing a pointer to a value. [Go 1.26 language changes](https://go.dev/doc/go1.26#language)

**Bad — scaffolding hides one optional value**

```go
options := Options{
	Retries: func() *int {
		value := 3
		return &value
	}(),
}
```

**Good**

```go
options := Options{
	Retries: new(3),
}
```

**Boundary:** Assume `Retries *int`, with nil meaning unspecified. `new(value)` creates a new variable initialized from that value; `&existing` points to the existing variable. They are not interchangeable when aliasing matters. This API does not justify pointer fields where an ordinary value suffices.

### GOR-051 — Use typed JSON models and choose JSON behavior deliberately

**SHOULD.** Model known payloads with structs and explicit tags. Validate business rules after decoding. On Go 1.27, JSON v2 is available in the standard release, but changing a v1 import is a compatibility decision, not a formatting cleanup. [Go 1.27](https://go.dev/doc/go1.27) and [JSON v2](https://pkg.go.dev/encoding/json/v2)

**Bad — a known schema is reconstructed through assertions**

```go
var raw map[string]any
if err := json.Unmarshal(data, &raw); err != nil {
	return err
}
customerID := raw["customer_id"].(string)
```

**Good — v2; this API deliberately rejects unknown fields**

```go
// Import: "encoding/json/v2".
type CreateInvoiceRequest struct {
	CustomerID  string `json:"customer_id"`
	AmountMinor int64  `json:"amount_minor"`
}

var request CreateInvoiceRequest
if err := json.Unmarshal(data, &request, json.RejectUnknownMembers(true)); err != nil {
	return fmt.Errorf("decode invoice request: %w", err)
}
if request.CustomerID == "" {
	return ErrMissingCustomerID
}
```

**Boundary:** Strict unknown-field handling is policy, not a universal default. Test nil collections, omitted fields, name matching, duplicate names, custom marshalers, and deterministic output before migrating JSON versions. Do not assume v1 and v2 are drop-in equivalents. Bound externally supplied body sizes at the transport boundary.

### GOR-052 — Use I/O contracts completely

**MUST.** Handle terminal scanner errors, buffered flush failures, and relevant short-write behavior. **SHOULD.** Prefer established `io` and `bufio` operations to hand-written transfer loops. Accept the smallest appropriate I/O interface.

**Bad — read failure looks like successful end of input**

```go
scanner := bufio.NewScanner(reader)
for scanner.Scan() {
	if err := consume(scanner.Text()); err != nil {
		return err
	}
}
return nil
```

**Good**

```go
scanner := bufio.NewScanner(reader)
for scanner.Scan() {
	if err := consume(scanner.Text()); err != nil {
		return err
	}
}
if err := scanner.Err(); err != nil {
	return fmt.Errorf("scan records: %w", err)
}
return nil
```

**Boundary:** Scanner has a token-size limit; configure it deliberately or use a reader suitable for large records. A buffered writer needs a checked `Flush`; closing its underlying file does not flush the buffer for you. `io.Copy` is usually clearer than inventing a read/write loop. [bufio](https://pkg.go.dev/bufio) and [io](https://pkg.go.dev/io)

## Tests and benchmarks

### GOR-053 — Make test scenarios and failures describe behavior

**SHOULD.** Give tests and subtests meaningful names. Use a table when cases share the same procedure, with visible inputs and expected results. Keep distinct workflows in separate tests. [Subtests](https://go.dev/blog/subtests)

**Bad — the failed case is hard to identify**

```go
func TestParse(t *testing.T) {
	got, err := parseLimit("12")
	if err != nil || got != 12 {
		t.Fatal("bad result")
	}
	_, err = parseLimit("many")
	if err == nil {
		t.Fatal("bad result")
	}
}
```

**Good**

```go
func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "integer", input: "12", want: 12},
		{name: "not a number", input: "many", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLimit(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLimit(%q) error = %v; wantErr %t",
					tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseLimit(%q) = %d; want %d", tt.input, got, tt.want)
			}
		})
	}
}
```

**Boundary:** The test calls GOR-031's example function. Check specific error identity/type when that is the contract; `wantErr` alone only checks presence. Avoid building a mini test framework to save a few lines.

### GOR-054 — Compare values using the intended equality

**SHOULD.** Choose comparisons that match the contract: scalar `==`, `bytes.Equal`, `slices.Equal`, `maps.Equal`, or explicit domain comparisons. Use a comparison library if its useful diffs justify the dependency. Avoid serialized or formatted string comparisons for structured values.

**Bad — formatting can hide structural differences**

```go
if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
	t.Fatal("different IDs")
}
```

**Good**

```go
if !slices.Equal(gotIDs, wantIDs) {
	t.Errorf("IDs = %q; want %q", gotIDs, wantIDs)
}
```

**Boundary:** `slices.Equal` considers nil and empty equal; check nilness separately if the contract distinguishes it. Map equality does not imply deep equality for referenced objects. Floating-point NaN and `time.Time` need appropriate semantics. [slices.Equal](https://pkg.go.dev/slices#Equal) and [maps.Equal](https://pkg.go.dev/maps#Equal)

### GOR-055 — Synchronize concurrent tests instead of guessing with sleep

**SHOULD.** Use channels, explicit completion, injected clocks, or `testing/synctest` to establish the state being asserted. Use `synctest` for isolated in-process concurrent behavior that fits its model. [testing/synctest](https://pkg.go.dev/testing/synctest)

**Bad — a scheduler guess and an unsynchronized shared variable**

```go
stopped := false
go func() {
	<-ctx.Done()
	stopped = true
}()
cancel()
time.Sleep(20 * time.Millisecond)
if !stopped {
	t.Fatal("worker did not stop")
}
```

**Good — Go 1.25+**

```go
func TestWorkerStopsOnCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			<-ctx.Done()
		}()

		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("worker did not stop after cancellation")
		}
	})
}
```

**Boundary:** This is a minimal synchronization example; a product test should exercise the real worker. `synctest` does not make external services deterministic. Prefer fakes or controlled integration fixtures for network, database, and process interactions.

### GOR-056 — Make helpers, cleanup, and parallel test state explicit

**SHOULD.** Use `t.Helper` so failure locations point to callers. Use `t.Cleanup` for fixture lifetime and `t.TempDir` for temporary directories. Enable `t.Parallel` only when shared mutable state, environment, and external resources are safe. [testing documentation](https://pkg.go.dev/testing)

**Bad — cleanup happens before the caller can use the fixture**

```go
func testServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(handler())
	defer server.Close()
	return server
}
```

**Good**

```go
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler())
	t.Cleanup(server.Close)
	return server
}
```

**Boundary:** The fixture lives through the test and its subtests. `t.Context()` is canceled before cleanup callbacks run; cleanup needing a live context should use its own bounded context. Do not combine process-wide environment mutation with parallel test assumptions.

### GOR-057 — Benchmark the operation you claim to measure

**SHOULD.** Use `testing.B.Loop` for straightforward benchmarks on Go 1.24+. Keep setup outside the measured loop when measuring only processing. Optimize readability tradeoffs only after a relevant benchmark or profile identifies a real cost. [B.Loop](https://go.dev/blog/testing-b-loop)

**Bad — fixture creation dominates a parse-only benchmark**

```go
func BenchmarkParseInvoice(b *testing.B) {
	for b.Loop() {
		data := invoiceFixture()
		if _, err := parseInvoice(data); err != nil {
			b.Fatal(err)
		}
	}
}
```

**Good — assume parsing does not mutate data**

```go
func BenchmarkParseInvoice(b *testing.B) {
	data := invoiceFixture()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseInvoice(data); err != nil {
			b.Fatal(err)
		}
	}
}
```

**Boundary:** Include setup in the loop if end-to-end cost is the question. Reset mutable input appropriately. `b.N`-based benchmarks remain valid; do not mix both loop-control styles in one benchmark.

## Version policy, tooling, and reviews

### GOR-058 — Declare the language baseline separately from toolchain choice

**MUST.** Keep `go.mod`, source syntax, dependency requirements, and CI toolchains compatible. **SHOULD.** Record the minimum supported version deliberately and use a reviewed current patch in CI. A `toolchain` directive suggests a toolchain; it is not an immutable CI pin. [Go toolchains](https://go.dev/doc/toolchain)

**Bad — file uses a generic method but module declares Go 1.25**

```go
// go.mod says: go 1.25.0
func (f Formatter) Format[T any](value T, render func(T) string) string {
	return f.Prefix + render(value)
}
```

**Good — a module intentionally adopting Go 1.27**

```gomod
module example.com/billing

go 1.27.0

toolchain go1.27.1
```

**Boundary:** A library supporting Go 1.26 should retain that baseline and avoid Go 1.27-only syntax. For an application that requires fixes from 1.27.1, setting `go 1.27.1` can express that minimum. Test the declared minimum where compatibility is promised. Do not use experimental features as mandatory readability rules.

### GOR-059 — Automate mechanical checks and review modernization diffs

**SHOULD.** Format automatically and run meaningful static and behavioral checks. Review `go fix` output as a code change. Treat complexity and length metrics as prompts for review, not proof of poor readability. [go vet](https://pkg.go.dev/cmd/vet) and [Go modernizers](https://go.dev/doc/go1.26#tools)

**Bad — formatting is the only check after changing source**

```sh
go fmt ./...
```

**Good — run from the relevant module root**

```sh
go fmt ./...
go vet ./...
go test ./...
```

**Boundary:** Add `go test -race ./...` where concurrent code and a supported runner justify it. The race detector observes executed paths; a pass does not prove absence of all races. Pin optional analyzers, including their Go-version support, and document suppressions. [Race detector](https://go.dev/doc/articles/race_detector)

### GOR-060 — Make reviews specific, traceable, and behavior-aware

**SHOULD.** Cite the rule, identify the concrete reading or correctness problem, and show the smallest useful change. Keep rules, examples, and exceptions traceable when revising this guide. Do not silently replace specific requirements with vague “idiomatic Go” advice.

**Bad — the review demands arbitrary brevity**

```go
// REVIEW: Too many lines. Rewrite this as a clever one-liner.
if err != nil {
	return err
}
return nil
```

**Good — the review explains a precise simplification**

```go
// REVIEW (GOR-015): Both branches return the existing err value.
// Returning it directly preserves the nil-success behavior.
return err
```

**Boundary:** Simplify only if the surrounding control flow makes the equivalence true. Label behavior changes separately from style fixes. A legitimate exception should explain the constraint, not merely silence a linter.

## Practical adoption

### Review priority

| Priority | Resolve first | Typical rules |
| --- | --- | --- |
| 1 | Incorrect results, dropped failures, races, leaks, broken contracts | 016, 020, 028–032, 034–044, 047, 052 |
| 2 | Hard-to-follow decisions, unclear APIs, hidden dependencies | 003–015, 017–027 |
| 3 | Ambiguous data semantics and weak tests | 042–057 |
| 4 | Mechanical consistency and safe modernization | 001–002, 058–060 |

These priorities help review work; they do not override a rule's stated requirement or a project's release gates.

### A small decision table

| Situation | Default choice | Keep the alternative when |
| --- | --- | --- |
| One yes/no decision | `if` / `if-else` | A different construct clarifies a larger decision |
| Several values of one expression | `switch value` | Branches are independent rather than alternatives |
| Several ordered related conditions | `switch` without an expression | Separate guards communicate failure order better |
| Collection membership | `slices.Contains` | Equality is domain-specific or another data structure fits |
| Filtering with effects, errors, or early exit | Direct loop | A short established iterator API is clearer |
| Small domain operation | Concrete function or method | Real shared behavior justifies an interface or generic |
| Consumer substitution | Small behavioral interface | No abstraction is currently needed |
| Same algorithm across element types | Generic function | Concrete types explain domain rules better |
| Type-local generic operation | Generic method on Go 1.27+ | Earlier compatibility or a free function is clearer |
| Expected operation failure | Returned `error` | A documented invariant or `Must...` contract applies |
| Request-scoped I/O | Propagated `context.Context` | Explicitly owned background work has its own lifetime |
| Shared mutable memory | Simple synchronization with clear ownership | A message-passing design better matches the workflow |
| Output order matters | Explicit sorting/order | The data source already guarantees the required order |

### Local checks and CI

For routine work, run the GOR-059 commands from each relevant module root. Use the project's existing checks rather than installing an unrelated all-purpose toolchain.

In a clean CI checkout, a simple tracked-source formatting check is:

```sh
go fmt ./...
git diff --exit-code -- '*.go'
```

For concurrency-sensitive packages on a supported runner:

```sh
go test -race ./...
```

For an intentional modernization pass, start from a clean working tree or an isolated checkout so that the generated changes can be reviewed on their own:

```sh
go fix ./...
git diff
go fmt ./...
go vet ./...
go test ./...
```

`go fix` modifies source. Inspect every proposed migration for compatibility and readability before committing it. Generated files should be regenerated through their generator; avoid hand edits that will be lost. Respect build tags, platform-specific files, and each module's own baseline when defining CI coverage.

Editor formatting on save is useful. Optional import organization and additional static analyzers can complement the standard tools, but their versions and enabled checks should be reviewed and recorded. Avoid redundant formatters with conflicting output.

### Pull-request checklist

- [ ] Names express domain meaning; package and exported names read well together.
- [ ] `switch` is used for several alternatives where appropriate; simple binary decisions remain `if`.
- [ ] The main path, early exits, evaluation order, and side effects are easy to follow.
- [ ] Functions and abstractions remove meaningful complexity rather than only lines.
- [ ] Required data, dependency ownership, and mutation are visible in the API.
- [ ] Errors preserve the intended cause contract and reach a deliberate handling boundary.
- [ ] Resource cleanup covers success, failure, cancellation, and early exit.
- [ ] Goroutines, blocking operations, and shared state have clear lifetime and synchronization rules.
- [ ] Nil versus empty, map ordering, shallow copies, units, and JSON behavior remain intentional.
- [ ] Tests describe behavior, have useful diagnostics, and avoid timing guesses.
- [ ] New syntax and library calls fit the module's declared baseline.
- [ ] Formatting and applicable checks passed; any unrun checks are stated accurately.
- [ ] Public API or behavior changes are called out separately from readability changes.

### Reusable review instruction

```text
Review the changed Go code against go-readability-style-guide.md.

For each substantive finding, report:
- rule ID and whether it is a correctness issue or readability preference;
- location and the concrete difficulty or failure mode;
- the smallest justified improvement;
- any change to public APIs, errors, nilness, ordering, mutation,
  allocation, I/O, cancellation, or concurrency;
- the evidence or validation needed for that change.

Keep switch-over-multi-branch-if guidance explicit, and retain if for binary
decisions. Do not introduce generics, interfaces, concurrency, or extraction
solely to reduce line count. Respect documented exceptions and the module's
Go version. Preserve existing behavior unless a behavior change is intended.
```

## Maintenance and verification notes

**Stable identifiers:** Keep existing GOR IDs when editing a rule. Add new IDs for new rules. Record changes to a rule's requirement, examples, exceptions, or version requirements. Never silently remove guidance while consolidating sections.

**Version updates:** Check the official release history, release notes, and the specific package documentation. Review whether a previously experimental API became supported, whether an old restriction changed, and whether a migration changes behavior. Effective Go is useful background but does not comprehensively cover modern Go features. [Effective Go's scope](https://go.dev/doc/effective_go)

**Validation for this edition:** Release and version-sensitive API guidance were checked against official Go sources. Examples were reviewed as instructional snippets. No Go toolchain was available in the authoring environment, so these snippets were not compiler-tested; adapt them into complete package context and run the listed checks before adopting code.

| Edition | Date | Change |
| --- | --- | --- |
| 1.0 | 2026-09-13 | Initial 60-rule guide with paired examples, explicit exceptions, Go 1.27 coverage, review checklist, and tooling workflow. |
