package testhelper

import "io"

func CloseWithDefer(c io.Closer) {
	doClose(c)
}

func doClose(c io.Closer) {
	c.Close()
}
