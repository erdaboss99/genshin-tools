module github.com/erdaboss99/gensin-tools

go 1.27.3

require (
	github.com/erdaboss99/gensin-tools/control v0.0.0
	github.com/erdaboss99/gensin-tools/ocr v0.0.0
)

// These keep builds working with GOWORK=off and "go mod tidy", which ignore go.work.
replace (
	github.com/erdaboss99/gensin-tools/control => ./control
	github.com/erdaboss99/gensin-tools/ocr => ./ocr
)
