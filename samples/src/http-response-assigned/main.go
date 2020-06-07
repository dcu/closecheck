package main

import (
	"context"
	"io"
	"net/http"
)

func doReq() *http.Response {
	res, err := http.Get("https://www.google.com")
	if err != nil {
		panic(err)
	}

	return res
}

type closer struct {
}

func (c closer) closeBody(bodyToBeClosed io.Closer) { // want closeBody:"is not closer"
	c.closeBodyWithContext(context.Background(), bodyToBeClosed)
}

func (c closer) closeBody2(bodyToBeClosed io.Closer) { // want closeBody2:"is closer"
	if bodyToBeClosed != nil {
		bodyToBeClosed.Close()
	}
}

func (c closer) closeBodyWithContext(ctx context.Context, bodyToBeClosed io.Closer) { // want closeBodyWithContext:"is closer"
	if err := bodyToBeClosed.Close(); err != nil {
		panic("this shouldn't happen")
	}
}

func (c closer) doNothing(interface{}) {
}

var aCloser = closer{}

func main() {
	res := doReq()

	defer aCloser.closeBody(res.Body)

	res2 := doReq()
	_ = res2.Body.Close()

	res3 := doReq()
	if res3.Body != nil {
		defer res3.Body.Close()
	}

	res4 := doReq()
	if err := res4.Body.Close(); err != nil {
		println("failed to close res4")
	}

	res5 := doReq()
	defer aCloser.closeBody2(res5.Body)

	doReq() // want `return value won't be closed because it wasn't assigned`

	res6 := doReq()
	aCloser.doNothing(res6.Body.Close())
}
