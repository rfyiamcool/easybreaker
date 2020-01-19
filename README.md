# easybreaker

the circuit breaker can prevent an application from repeatedly trying to execute an operation that's likely to fail.
It is implemented as a state machine with the following states:

- `Closed`: the request from the application is allowed to pass.
- `Half-Open`: a limited number of requests are allowed to pass.
- `Open`: the request is failed immediately and an error returned.

## Usage

new circuit breaker

```go
func New(interval time.Duration, cooldown time.Duration, fns ...OptionCall) (*Breaker, error) {
```

more option

```go
func WithLeastReqs(atLeastReqs uint32) OptionCall {
func WithStateFunc(toOpen, toClosed ToState) OptionCall {
```

Execute runs a given request if the circuit breaker accepts it,
cases when it's in the closed state, or half-open one
and the number of requests has not yet reached `atLeastReqs`.

Returns ErrBreakerOpen when it doesn't accept the request,
otherwise the error from the req function:

```go
func (b *Breaker) Execute(req func() error) error
```

## Example

```go
package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/rfyiamcool/easybreaker"
)

// open the circuit breaker in case of 5% of failed requests
toOpen := func(total uint32, failures uint32) bool {
	return total > 0 && float64(failures)/float64(total) >= 0.05
}

// close the circuit breaker only if no failures
toClosed := func(total uint32, failures uint32) bool {
	return failures == 0
}

var (
	breaker *Breaker
)

func init() {
	var err error
	breaker, err = easybreaker.New(
		time.Minute, 10*time.Second, 
		WithLeastReqs(100), WithStateFunc(toOpen, toClosed),
	)
	if err != nil {
		panic(err)
	}
}

func getStatus(url string) (string, error) {
	var resp *http.Response
	err = b.Execute(func() error {
		resp, err = http.Get(url)
		return err
	})

	if err == circuit.ErrBreakerOpen {
		return "200 (cache)", nil
	}

	if err != nil {
		return "", err
	}

	return resp.Status, nil
}

func main() {
	var err error
	for {
		err = getStatus()
		if err != nil {
			fmt.Println(err)
		}
		time.Sleep(1 * time.Second)
	}
}
```
