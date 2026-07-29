package cli

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/spf13/cobra"
)

type commandError struct {
	code        string
	jsonMessage string
	err         error
}

func (e *commandError) Error() string {
	return e.err.Error()
}

func (e *commandError) Unwrap() error {
	return e.err
}

func withErrorCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &commandError{code: code, jsonMessage: err.Error(), err: err}
}

type jsonErrorEnvelope struct {
	Error jsonErrorBody `json:"error"`
}

type jsonErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func commandRequestsJSON(cmd *cobra.Command) bool {
	if cmd == nil || cmd.Flags().Lookup("json") == nil {
		return false
	}
	enabled, err := cmd.Flags().GetBool("json")
	return err == nil && enabled
}

func commandErrorCode(err error) string {
	var coded *commandError
	if errors.As(err, &coded) {
		return coded.code
	}
	if errors.Is(err, ErrInternal) {
		return "internal_error"
	}
	return "invalid_arguments"
}

func commandErrorMessage(err error) string {
	var coded *commandError
	if errors.As(err, &coded) {
		return coded.jsonMessage
	}
	return err.Error()
}

func renderCommandError(cmd *cobra.Command, err error) {
	if commandRequestsJSON(cmd) {
		envelope := jsonErrorEnvelope{
			Error: jsonErrorBody{
				Code:    commandErrorCode(err),
				Message: commandErrorMessage(err),
			},
		}
		if encodeErr := writeJSON(cmd.ErrOrStderr(), envelope); encodeErr != nil {
			cmd.PrintErrf("Error: %v\n", encodeErr)
		}
		return
	}
	cmd.PrintErrln("Error:", err)
}
