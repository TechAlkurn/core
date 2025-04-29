package lib

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"log"
	"os"
)

// function to check if file exists
func FileIsNotExist(fileName string) bool {
	_, err := os.Stat(fileName)
	return os.IsNotExist(err)
}

func FileIsExist(fileName string) bool {
	_, err := os.Stat(fileName)
	return !os.IsNotExist(err)
}

func DirIsExist(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func WriteCsv(fileName string, header []string, data [][]any) bool {
	csvFile, err := os.Create(fileName)
	if err != nil {
		log.Fatalf("failed to create file: %s", err)
	}

	csvwriter := csv.NewWriter(csvFile)
	defer csvFile.Close()   // Close the file when the function exits
	defer csvwriter.Flush() // Flush the writer when the function exits

	csvwriter.Write(header)
	var entries [][]string

	for _, entry := range data {
		var data []string
		for _, entity := range entry {
			data = append(data, fmt.Sprintf("%v", entity))
		}
		entries = append(entries, data)
	}
	if err := csvwriter.WriteAll(entries); err != nil {
		log.Fatalf("failed to write file: %s", err)
	}

	return true
}

func ReadCsv(fileName string) ([][]string, error) {
	file, err := os.Open(fileName)
	if err != nil {
		log.Fatalf("failed to reading file: %s", err)
	}
	return csv.NewReader(file).ReadAll()
}


func WriteFile(fileName string, buf *bytes.Buffer) {
	file, err := os.Create(fileName)
	if err != nil {
		log.Fatalf("failed to reading file: %s", err)
	}
	file.Write(buf.Bytes())
	defer file.Close() // Close the file when the function exits
}

func DeleteFile(fileName string) error {
	if err := os.Remove(fileName); err != nil {
		return err
	}
	return nil
}

func ReadJSONFile(fileName string) (data any, err error) {
	jsonData, err := os.ReadFile(fileName)
	if err != nil {
		log.Fatalf("Error reading JSON file: %v", err)
	}
	// Define a variable to store the decoded JSON data
	// Unmarshal the JSON data into the struct
	err = json.Unmarshal(jsonData, &data)
	if err != nil {
		log.Fatalf("Error unmarshalling JSON: %v", err)
	}
	return
}

func ReadContentFile(fileName string) (content string, err error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		log.Fatalf("Error reading JSON file: %v", err)
	}
	// Define a variable to store the decoded JSON data
	// Unmarshal the JSON data into the struct
	content = string(data)
	return
}

