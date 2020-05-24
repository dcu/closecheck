# closecheck

`closecheck` is a static code analyzer which checks whether a return value that implements `io.Closer` is correctly closed

## Install

You can get `closecheck` by `go get` command.

```bash
$ go get -u github.com/dcu/closecheck
```

## QuickStart

```bash
$ closecheck package/...
```

## Analyzer

`closecheck` checks that a returned `io.Closer` is not ignored since that's a common cause of bugs and leaks in Go applications. Specially when dealing with `*http.Response.Body`

For example, the following code is reported as an error:

```go
_, err := http.Get("https://www.google.com")
...
```
Because the response body must be closed.

The checker detects whether the returned struct has a field that implements `io.Closer` or not. In the previous case the `*http.Response` struct has a field called `Body` which implements `io.Closer`
