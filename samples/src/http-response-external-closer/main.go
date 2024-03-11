package main

import (
	"net/http"

	"github.com/nikoremi97/closecheck/samples/src/testhelper"
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
