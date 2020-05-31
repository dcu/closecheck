package main

import "net/http"

func main() {
	go http.Get("https://www.google.com") // want `return value won't be closed because it's on go statement`
}
