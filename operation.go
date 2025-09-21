package mockfs

// Operation defines the type of filesystem operation for error injection context.
type Operation int

const (
	// InvalidOperation is an invalid operation.
	InvalidOperation Operation = iota - 1

	OpStat      // OpStat represents the Stat operation.
	OpOpen      // OpOpen represents the Open operation.
	OpRead      // OpRead represents the Read operation.
	OpWrite     // OpWrite represents the Write operation.
	OpReadDir   // OpReadDir represents the ReadDir operation.
	OpClose     // OpClose represents the Close operation.
	OpMkdir     // OpMkdir represents the Mkdir operation.
	OpMkdirAll  // OpMkdirAll represents the MkdirAll operation.
	OpRemove    // OpRemove represents the Remove operation.
	OpRemoveAll // OpRemoveAll represents the RemoveAll operation.
	OpRename    // OpRename represents the Rename operation.

	// NumOperations is the number of available operations.
	NumOperations
)

// operationNames maps each operation to a human-readable string.
var operationNames = map[Operation]string{
	OpStat:      "Stat",
	OpOpen:      "Open",
	OpRead:      "Read",
	OpWrite:     "Write",
	OpReadDir:   "ReadDir",
	OpClose:     "Close",
	OpMkdir:     "Mkdir",
	OpMkdirAll:  "MkdirAll",
	OpRemove:    "Remove",
	OpRemoveAll: "RemoveAll",
	OpRename:    "Rename",
}

// String returns a human-readable string representation of the operation.
// This is used for logging and testing purposes.
func (op Operation) String() string {
	if op < 0 || op >= NumOperations {
		return ""
	}

	return operationNames[op]
}

// StringToOperation converts a string to an Operation.
// It returns an invalid operation if the string does not match a valid operation.
func StringToOperation(s string) Operation {
	for op := Operation(0); op < NumOperations; op++ {
		if operationNames[op] == s {
			return op
		}
	}
	return -1
}
