package main

import (
	"net/http"

	"github.com/dcu/closecheck/samples/src/testhelper"
)

func doReq() *http.Response {
	res, err := http.Get("https://www.google.com")
	if err != nil {
		panic(err)
	}

	return res
}

func main() {
	res := doReq()

	testhelper.CloseWithDefer(res.Body)
}
