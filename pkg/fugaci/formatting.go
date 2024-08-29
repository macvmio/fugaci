package fugaci

import (
	"encoding/json"
	"log"
)

// PrettyPrintStruct uses the json.MarshalIndent function to pretty print a struct.
func PrettyPrintStruct(i interface{}) {
	// MarshalIndent struct to JSON with pretty print
	jsonData, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		log.Fatalf("Error occurred during marshaling. Error: %s", err.Error())
	}
	log.Println(string(jsonData))
}
