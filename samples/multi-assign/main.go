package main

type customCloser struct {
}

// Close implements the io.Closer interface
func (c *customCloser) Close() error {
	return nil
}

func closer() *customCloser {
	return &customCloser{}
}

func main() {
	_, _ = 1, closer()
}
