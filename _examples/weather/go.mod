module weather

go 1.24

toolchain go1.24.5

replace github.com/kataras/httpclient => ../../

require (
	github.com/kataras/golog v0.1.7
	github.com/kataras/httpclient v0.0.0-00010101000000-000000000000
)

require (
	github.com/kataras/pio v0.0.10 // indirect
	golang.org/x/sys v0.0.0-20221006211917-84dc82d7e875 // indirect
	golang.org/x/time v0.12.0 // indirect
)
