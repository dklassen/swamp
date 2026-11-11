# Testing

- TDD (red-green-refactor) is a project-wide requirement: write a failing test, watch it fail for the right reason, write minimal code to pass, watch it pass, then refactor. One behavior at a time — never write multiple tests before running any of them. Don't move on from a failure, and don't `.skip()` a test you couldn't figure out.
- Before marking any test task complete, record `VERIFY: Ran [exact test name] — Result: [PASS/FAIL/DID NOT RUN]`. DID NOT RUN can't be marked complete.
- Use table-driven tests for any function with multiple input/output cases.
- Use `t.Run` subtests for logically grouped cases, and `t.Parallel()` for independent cases.
- Use `t.Helper()` in helper functions to improve test failure output.
- Focus on testing behavior, not implementation details. Avoid testing unexported functions or internal state unless there's a compelling reason.
- Prefer `github.com/google/go-cmp/cmp` (`cmp.Diff`) over manual field-by-field comparison or `==` on structs containing `time.Time` (`==` on `time.Time` is a known footgun — see Go's own docs).
- Arrange state through the same service calls production uses, and assert what a caller can observe. Never set up or check state through `internal/repository`, `sqlcdb`, raw SQL or test-only methods; a state with no service path is a question for Bob, not a backdoor. Don't assert counts that hold only because setup skipped a side effect (assert the change an action caused), or compare two outputs of the same code path. The exceptions are `db/migrations` and `store` connection tests, whose subject is the schema or the connection.
- Take expected names from the constants (`x.String()`), not string literals, so a rename that changes no behaviour breaks no test.
