package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9\-_ ]+`)

func clearString(str string) string {
	return str
}

func getDefaultValue(variableType string, state bool) any {
	switch strings.ToLower(variableType) {
	case "string":
		return ""
	case "number":
		if state {
			return 1
		}
		return 0
	case "boolean":
		return state
	default:
		return ""
	}
}

func main() {
	var dvcProject string
	// Read in the first argument as the file path
	filePath := os.Args[1]

	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}

	var tlImport TLImportFormat
	err = json.Unmarshal(fileContent, &tlImport)
	if err != nil {
		fmt.Println("Error parsing file:", err)
		return
	}
	if tlImport.TLProject == "" {
		fmt.Println("TLProject is required")
		return
	} else {
		dvcProject = tlImport.DVCProject
	}

	// Filter out features with no variables
	var cleanedTLImport []TLImportRecord
	for _, feature := range tlImport.Records {
		if len(feature.Variables) == 0 {
			continue
		}
		cleanedTLImport = append(cleanedTLImport, feature)
	}

	mergedFeatures := make(map[string]TLImportRecord)
	for _, feature := range cleanedTLImport {
		if _, ok := mergedFeatures[feature.FeatureName]; !ok {
			mergedFeatures[feature.FeatureName] = feature
		} else {
			if mergedFeatures[feature.FeatureName].FeatureName != feature.FeatureName {
				merged := mergedFeatures[feature.FeatureName]
				merged.Variables = append(merged.Variables, feature.Variables...)
				mergedFeatures[feature.FeatureName] = merged
			}
		}
	}

	dvcApi := newDevCycleAPI()

	fmt.Println("Importing", len(mergedFeatures), "features from Taplytics to DevCycle")
	fmt.Println("DevCycle Project:", dvcProject)
	fmt.Println("CustomData Properties:", tlImport.GetCustomDataProperties())

	// Now create features with targeting rules
	if err = dvcApi.importFeaturesToDevCycle(dvcProject, mergedFeatures); err != nil {
		fmt.Printf("Error importing features: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Import completed successfully!")
}
