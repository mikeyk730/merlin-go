package processor

import (
	"encoding/csv"
	"io"
	"strings"
	"os"

	"github.com/tphakala/birdnet-go/internal/conf"
	"github.com/tphakala/birdnet-go/internal/errors"
)

var s_life_list = map[string]bool{}

func loadLifeList(settings *conf.Settings) error {
	path := settings.SoundId.LifeListPath
	if path == "" {
		return errors.Newf("Life list path is not set in the configuration").
			Component("life_list").
			Category(errors.CategoryFileIO).
			Build()
	}
	
	file, err := os.Open(path)
	if err != nil {
		return errors.New(err).
			Component("life_list").
			Category(errors.CategoryFileIO).
			Context("operation", "open").
			Build()
	}
	defer file.Close()

	reader := csv.NewReader(file)

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			return errors.New(err).
				Component("life_list").
				Category(errors.CategoryFileIO).
				Context("operation", "read").
				Build()
		}

		s_life_list[strings.ToLower(record[4])] = true
	}
	
	return nil
}

func isInLifeList(scientificName string) bool {
	_, exists := s_life_list[strings.ToLower(scientificName)]
	return exists
}