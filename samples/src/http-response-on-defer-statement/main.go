package main

import "net/http"

func main() {
	defer http.Get("https://www.google.com") // want "return value won't be closed because it's on defer statement"

	res, _ := http.Get("https://www.google.com")

	defer func() {
		_ = res.Body.Close()
	}()
}
