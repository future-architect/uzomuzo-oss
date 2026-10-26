module example.com/myapp

go 1.23.2

require (
	github.com/exact/match v1.0.0
	github.com/other/version v1.0.0 // indirect
	github.com/both/forms v1.0.0
)

replace github.com/exact/match v1.0.0 => github.com/exact/fork v1.0.1

replace github.com/other/version v1.5.0 => github.com/other/fork v1.5.1

replace github.com/both/forms => github.com/both/any-version v1.9.0

replace github.com/both/forms v1.0.0 => github.com/both/this-version v1.0.1
