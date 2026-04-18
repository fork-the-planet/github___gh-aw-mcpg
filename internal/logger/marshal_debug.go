package logger

import "encoding/json"

// LogMarshaledForDebug marshals value for debug logging and dispatches to the
// provided callbacks for success or marshal failure paths.
func LogMarshaledForDebug(value interface{}, onMarshalSuccess func(string), onMarshalFailure func(error)) {
	resultJSON, err := json.Marshal(value)
	if err != nil {
		onMarshalFailure(err)
		return
	}
	onMarshalSuccess(string(resultJSON))
}
