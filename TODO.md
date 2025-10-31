# TODO for future improvements

## The `splitDDLs` function

The `splitDDLs()` function in `database/parser.go` has comments:

```go
// Right now, the parser isn't capable of splitting statements by itself.
// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
```

So once the parser is capable of splitting statements by itself, we can remove this function.

