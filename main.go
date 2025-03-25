package main

import (
	"encoding/json"
	"fmt"
	"github.com/ettle/strcase"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type TLVariable struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type TLImportFormat struct {
	TLProject  string           `json:"tl_project"`
	DVCProject string           `json:"dvc_project"`
	Records    []TLImportRecord `json:"records"`
}

type TLImportRecord struct {
	ID          string       `json:"_id"`
	FeatureName string       `json:"featureName"`
	Variables   []TLVariable `json:"variables"`
	Tags        []string     `json:"tags"`
}

type DevCycleVariable struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Key         string `json:"key"`
	Type        string `json:"type,omitempty"`
}

type DevCycleVariation struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Variables map[string]any `json:"variables"`
}

type SdkVisibility struct {
	Mobile bool `json:"mobile"`
	Client bool `json:"client"`
	Server bool `json:"server"`
}

type DevCycleNewFeaturePOSTBody struct {
	Name          string              `json:"name"`
	Key           string              `json:"key"`
	Description   string              `json:"description"`
	Variables     []DevCycleVariable  `json:"variables"`
	Variations    []DevCycleVariation `json:"variations"`
	SdkVisibility SdkVisibility       `json:"sdkVisibility"`
	Type          string              `json:"type"`
	Tags          []string            `json:"tags"`
}

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

	client := http.DefaultClient
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

	var cleanedTLImport []TLImportRecord
	// Clean up the feature names for merging
	for _, feature := range tlImport.Records {
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

	var newFeatures []DevCycleNewFeaturePOSTBody
	for _, feature := range mergedFeatures {
		var variables []DevCycleVariable
		for _, variable := range feature.Variables {
			key := fmt.Sprintf("%s%s", strcase.ToKebab(clearString(variable.Name)), "-taplytics")

			devCycleVariable := DevCycleVariable{
				Name: variable.Name,
				Key:  key,
				Type: variable.Type,
			}
			variables = append(variables, devCycleVariable)
		}
		variationOn := DevCycleVariation{
			Key:       "imported-on",
			Name:      "On",
			Variables: make(map[string]any),
		}
		variationOff := DevCycleVariation{
			Key:       "imported-off",
			Name:      "Off",
			Variables: make(map[string]any),
		}

		for _, variable := range feature.Variables {
			variableKey := fmt.Sprintf("%s%s", strcase.ToKebab(clearString(variable.Name)), "-taplytics")

			variationOn.Variables[variableKey] = getDefaultValue(variable.Type, true)
			variationOff.Variables[variableKey] = getDefaultValue(variable.Type, false)
		}
		newFeature := DevCycleNewFeaturePOSTBody{
			Name:        feature.FeatureName,
			Description: fmt.Sprintf("Imported from Taplytics - https://taplytics.com/dashboard/%s/featureFlags/%s/results", tlImport.TLProject, feature.ID),
			Key:         strings.ToLower(strcase.ToKebab(feature.FeatureName)),
			Variables:   variables,
			Variations:  []DevCycleVariation{variationOn, variationOff},
			Type:        "release",
			Tags: append([]string{
				fmt.Sprintf("%s", feature.ID),
				"taplytics-import",
			}, feature.Tags...),
			SdkVisibility: SdkVisibility{Mobile: true, Client: true, Server: true},
		}
		newFeatures = append(newFeatures, newFeature)
	}
	// Split the new features array into blocks of 20 to make it easier to import
	var chunkedFeatures [][]DevCycleNewFeaturePOSTBody
	for i := 0; i < len(newFeatures); i += 20 {
		end := i + 20
		if end > len(newFeatures) {
			end = len(newFeatures)
		}
		chunkedFeatures = append(chunkedFeatures, newFeatures[i:end])
	}
	fmt.Println("Importing", len(newFeatures), "features in", len(chunkedFeatures), "chunks")

	for _, chunk := range chunkedFeatures {
		chunkJSON, err := json.Marshal(chunk)
		if err != nil {
			fmt.Println("Error marshalling chunk:", err)
			return
		}
		req, err := http.NewRequest("POST", fmt.Sprintf("https://api.devcycle.com/v1/projects/%s/features/multiple", dvcProject), strings.NewReader(string(chunkJSON)))
		if err != nil {
			fmt.Println("Error creating request:", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("DEVCYCLE_API_TOKEN")))
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("Error sending request:", err)
			return
		}

		if resp.StatusCode != 201 {
			fmt.Println("Error importing features:", resp.Status)
			continue
		}
	}
}
