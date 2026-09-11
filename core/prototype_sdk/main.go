// Disposable SDK lifecycle proof; no SSH or command execution.
package main

import (
	"os"
	"strconv"

	"github.com/vibepwners/hovel/sdk/go/hovel"
)

type prototype struct{}

func (prototype) Info() hovel.Info {
	return hovel.Info{Name: "burrow-sdk-prototype", Version: "0.0.0", Type: hovel.TypeSurvey, Summary: "Inert SDK lifecycle proof."}
}

func (prototype) Schema() hovel.Schema { return hovel.Schema{} }

func (prototype) Run(ctx *hovel.Context) (hovel.Result, error) {
	ctx.Log.Info("opening inert prototype session")
	_, err := ctx.OpenSession(&hovel.LineShellSession{
		Prompt: "prototype> ",
		Handle: func(command string) (string, error) { return "inert: " + command, nil },
	}, hovel.WithName("Burrow inert prototype"))
	if err != nil {
		return hovel.Result{}, err
	}
	return hovel.Ok(nil, hovel.WithSummary("inert prototype pid=" + strconv.Itoa(os.Getpid()))), nil
}

func main() { hovel.Serve(prototype{}) }
