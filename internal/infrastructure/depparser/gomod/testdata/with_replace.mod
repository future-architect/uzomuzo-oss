module example.com/myapp

go 1.23.2

require (
	github.com/old/module v1.0.0
	github.com/gin-gonic/gin v1.10.0
)

replace github.com/old/module => github.com/new/module v2.0.0
