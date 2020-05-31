package main

import "net/http"

func main() {
	http.Get("https://www.google.com") // want `return value won't be closed because it wasn't assigned`
}
