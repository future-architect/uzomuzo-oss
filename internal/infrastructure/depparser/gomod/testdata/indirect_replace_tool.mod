module example.com/myapp

go 1.24

require github.com/direct/dep v1.0.0

require (
	github.com/old/indirect v0.1.0 // indirect
	golang.org/x/tools v0.30.0 // indirect
)

replace github.com/old/indirect => github.com/new/indirect v0.2.0

tool golang.org/x/tools/cmd/stringer
