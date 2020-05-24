package main

import (
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

func (c closer) closeBody(bodyToBeClosed io.Closer) {
	_ = bodyToBeClosed.Close()
}

var aCloser = closer{}

func main() {
	res := doReq()

	defer aCloser.closeBody(res.Body)
}
