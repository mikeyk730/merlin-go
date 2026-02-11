package processor

import (
	"encoding/csv"
	"log"
	"io"
	"strings"
	"os"

	"github.com/tphakala/birdnet-go/internal/conf"
)

var s_life_list = map[string]bool{}

func loadLifeList(settings *conf.Settings) {
	path := settings.SoundId.LifeListPath
	if path == "" {
		log.Fatal("Life list path is not set in the configuration")
	}
	
	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			log.Fatal(err)
		}

		s_life_list[strings.ToLower(record[4])] = true
	}
}

func isInLifeList(scientificName string) bool {
	_, exists := s_life_list[strings.ToLower(scientificName)]
	return exists
}