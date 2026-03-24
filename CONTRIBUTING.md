# Contributing to micromart

We welcome contributions! Thank you for taking the time to contribute to this project.

## How to Contribute

### Workflow

1. **Fork the repository**
   ```bash
   # Click "Fork" on GitHub, then:
   git clone https://github.com/ahargunyllib/micromart.git
   cd micromart
   ```

2. **Create a feature branch**
   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/bug-description
   ```

3. **Make your changes**
   - Write clean, idiomatic Go code
   - Follow existing code style and patterns
   - Add tests for new functionality
   - Update documentation as needed

4. **Test locally**
   ```bash
   # Run linter
   make lint

   # Run tests
   make test

   # Test end-to-end locally
   make up
   make migrate-up
   make run-gateway  # In separate terminal
   make run-order    # In separate terminal
   make run-inventory # In separate terminal

   # Test your changes
   curl http://localhost:8080/api/v1/...
   ```

5. **Commit with clear messages**
   ```bash
   git add .
   git commit -m "feat: add user authentication to gateway"
   # or
   git commit -m "fix: resolve race condition in saga rollback"
   ```

   **Commit message format:**
   - `feat:` - New feature
   - `fix:` - Bug fix
   - `docs:` - Documentation only
   - `refactor:` - Code refactoring
   - `test:` - Adding tests
   - `chore:` - Maintenance tasks

6. **Push and create PR**
   ```bash
   git push origin feature/your-feature-name
   ```
   Then open a Pull Request on GitHub.

## Pull Request Guidelines

### PR Requirements

- **Title**: Clear, descriptive summary of changes
- **Description**:
  - What problem does this solve?
  - How did you solve it?
  - Any breaking changes?
  - Screenshots/examples if applicable
- **Tests**: All PRs should include tests
- **CI**: Ensure GitHub Actions CI passes
- **Review**: Be responsive to feedback

### Before Submitting

- [ ] Code follows project style guidelines
- [ ] Tests added for new functionality
- [ ] All tests pass locally (`make test`)
- [ ] Linter passes (`make lint`)
- [ ] Documentation updated if needed
- [ ] Commit messages follow convention

## Code Style

### Go Code Standards

- Follow standard Go conventions ([Effective Go](https://go.dev/doc/effective_go))
- Use `gofmt` for formatting (enforced by CI)
- Keep functions small and focused (< 50 lines ideally)
- Prefer explicit error handling over panics
- Add comments for exported functions and complex logic
- Use meaningful variable names

### Examples

**Good:**
```go
// CalculateOrderTotal computes the total price for all items in an order.
// Returns the total in cents and any validation errors.
func CalculateOrderTotal(items []OrderItem) (int64, error) {
    if len(items) == 0 {
        return 0, ErrEmptyOrder
    }

    var total int64
    for _, item := range items {
        if item.Quantity <= 0 {
            return 0, fmt.Errorf("invalid quantity for item %s", item.ProductID)
        }
        total += item.UnitPriceCents * int64(item.Quantity)
    }

    return total, nil
}
```

**Bad:**
```go
// calc
func calc(i []OrderItem) (int64, error) {
    t := int64(0)
    for _, x := range i {
        t += x.UnitPriceCents * int64(x.Quantity) // No validation
    }
    return t, nil
}
```

## Testing Guidelines

### Test Structure

```go
func TestServiceName_MethodName(t *testing.T) {
    // Arrange - Set up test data and dependencies
    setup()

    // Act - Execute the code under test
    result, err := method()

    // Assert - Verify the results
    assert.NoError(t, err)
    assert.Equal(t, expected, result)
}
```

### Best Practices

- Write table-driven tests for multiple cases
- Use test fixtures or mocks for external dependencies
- Aim for >80% coverage on business logic
- Include integration tests for critical paths
- Use `t.Parallel()` when tests can run concurrently
- Clean up resources in `defer` or `t.Cleanup()`

### Example Table-Driven Test

```go
func TestCalculateOrderTotal(t *testing.T) {
    tests := []struct {
        name    string
        items   []OrderItem
        want    int64
        wantErr bool
    }{
        {
            name: "single item",
            items: []OrderItem{
                {ProductID: "123", Quantity: 2, UnitPriceCents: 1000},
            },
            want:    2000,
            wantErr: false,
        },
        {
            name:    "empty order",
            items:   []OrderItem{},
            want:    0,
            wantErr: true,
        },
        {
            name: "invalid quantity",
            items: []OrderItem{
                {ProductID: "123", Quantity: -1, UnitPriceCents: 1000},
            },
            want:    0,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := CalculateOrderTotal(tt.items)

            if tt.wantErr {
                assert.Error(t, err)
                return
            }

            assert.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

## Documentation

### What to Document

- **README.md**: Update if adding new features or changing setup
- **ADRs**: Add Architecture Decision Records for significant architectural choices
- **Code comments**: Comment exported types, functions, and complex logic
- **API docs**: Update API reference for endpoint changes
- **Examples**: Include usage examples in code comments

### Writing ADRs

Create a new ADR in `docs/adr/` for significant decisions:

```markdown
# ADR-XXX: Title

## Status
Accepted | Rejected | Deprecated | Superseded by ADR-YYY

## Context
What is the issue we're facing?

## Decision
What did we decide?

## Consequences
What are the trade-offs?
- Positive: ...
- Negative: ...
- Neutral: ...
```

## What to Contribute

### Good First Issues

These are great for new contributors:
- Documentation improvements
- Test coverage improvements
- Bug fixes with reproduction steps
- Performance optimizations with benchmarks
- Adding missing error handling
- Improving log messages

### Feature Ideas

We'd love help with:
- Additional payment methods in saga
- Product search improvements (fuzzy matching, filters)
- Rate limiting middleware
- Webhook notifications for order events
- Admin dashboard
- GraphQL API layer
- Caching strategies
- Database query optimizations

### Before Starting Major Work

**Please open an issue first** to discuss significant changes before investing time. This ensures:
- The feature aligns with project goals
- No duplicate work is happening
- We can discuss the approach
- You get early feedback

## Development Setup

See [DEVELOPMENT.md](DEVELOPMENT.md) for detailed development instructions including:
- Setting up your environment
- Running services locally
- Working with proto files
- Adding migrations
- Debugging tips

## Getting Help

### Questions?

- **General questions**: Open a [GitHub Discussion](https://github.com/ahargunyllib/micromart/discussions)
- **Bug reports**: File an [Issue](https://github.com/ahargunyllib/micromart/issues) with reproduction steps
- **Design questions**: Check existing [ADRs](docs/adr/) for architectural decisions

### Review Process

1. Maintainers will review PRs within 1-3 business days
2. Feedback will be provided via PR comments
3. Once approved, maintainers will merge
4. PRs may require multiple rounds of review

## Code of Conduct

### Our Standards

- Be respectful and inclusive
- Welcome newcomers
- Focus on what's best for the community
- Show empathy towards others
- Accept constructive criticism gracefully

### Unacceptable Behavior

- Harassment or discriminatory language
- Trolling or insulting comments
- Personal or political attacks
- Publishing others' private information
- Other unprofessional conduct

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

## Thank You!

Your contributions make this project better. We appreciate your time and effort! 🙏
