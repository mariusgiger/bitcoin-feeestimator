package btcutil

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/mariusgiger/bitcoin-feeestimator/pkg/feerate"

	"go.uber.org/zap"
)

type score struct {
	ScoreEconomical float64
	ScoreStandard   float64
	ScoreFast       float64
	NumberOfTxs     int
}

type prediction struct {
	feeRates          *feerate.FeeRates
	height            int
	economicalFeeRate float64
	standardFeeRate   float64
	fastFeeRate       float64
	scores            map[int]*score
}

type scores struct {
	predictions map[int]*prediction //blockheight->predictions

	logger *zap.Logger
}

func newScores(logger *zap.Logger) *scores {
	return &scores{
		logger:      logger,
		predictions: make(map[int]*prediction),
	}
}

func (s *scores) addPrediction(height int, rates *feerate.FeeRates, economicalFeeRate float64, standardFeeRate float64, fastFeeRate float64) {
	s.predictions[height] = &prediction{
		height:            height,
		feeRates:          rates,
		economicalFeeRate: economicalFeeRate,
		standardFeeRate:   standardFeeRate,
		fastFeeRate:       fastFeeRate,
		scores:            make(map[int]*score),
	}
}

func (s *scores) predictScores() error {
	for num, pred := range s.predictions {
		s.comparePredictionToNext10Blocks(num, pred)
	}

	return s.flush()
}

func (s *scores) flush() error {
	fileName := fmt.Sprintf("btcutilscores%v.csv", time.Now().Format(time.RFC3339))
	f, err := os.OpenFile("./output/"+fileName, os.O_CREATE|os.O_RDWR, 0660)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	err = w.Write([]string{
		"block_number",
		"priceEconomical",
		"priceStandard",
		"priceFast",
		"numberOfTxs",

		"scoreEconomicalPlus1",
		"scoreStandardPlus1",
		"scoreFastPlus1",

		"scoreEconomicalPlus2",
		"scoreStandardPlus2",
		"scoreFastPlus2",

		"scoreEconomicalPlus3",
		"scoreStandardPlus3",
		"scoreFastPlus3",

		"scoreEconomicalPlus4",
		"scoreStandardPlus4",
		"scoreFastPlus4",

		"scoreEconomicalPlus5",
		"scoreStandardPlus5",
		"scoreFastPlus5",

		"scoreEconomicalPlus6",
		"scoreStandardPlus6",
		"scoreFastPlus6",

		"scoreEconomicalPlus7",
		"scoreStandardPlus7",
		"scoreFastPlus7",

		"scoreEconomicalPlus8",
		"scoreStandardPlus8",
		"scoreFastPlus8",

		"scoreEconomicalPlus9",
		"scoreStandardPlus9",
		"scoreFastPlus9",

		"scoreEconomicalPlus10",
		"scoreStandardPlus10",
		"scoreFastPlus10",
	})

	if err != nil {
		return err
	}

	var records [][]string
	for blockHeight, prediction := range s.predictions {
		record := []string{
			strconv.Itoa(blockHeight),
			strconv.FormatFloat(prediction.economicalFeeRate, 'f', 3, 64),
			strconv.FormatFloat(prediction.standardFeeRate, 'f', 3, 64),
			strconv.FormatFloat(prediction.fastFeeRate, 'f', 3, 64),
			strconv.Itoa(prediction.feeRates.NumberOfTxs),
		}
		for i := blockHeight + 1; i < blockHeight+11; i++ {
			score, ok := prediction.scores[i]
			if !ok {
				record = append(record, strconv.Itoa(-1))
				record = append(record, strconv.Itoa(-1))
				record = append(record, strconv.Itoa(-1))
			} else {
				record = append(record, strconv.FormatFloat(score.ScoreEconomical, 'f', 3, 64))
				record = append(record, strconv.FormatFloat(score.ScoreStandard, 'f', 3, 64))
				record = append(record, strconv.FormatFloat(score.ScoreFast, 'f', 3, 64))
			}
		}

		records = append(records, record)
	}

	s.logger.Info("prediction score", zap.Any("scores", records))
	return w.WriteAll(records)
}

func (s *scores) comparePredictionToNext10Blocks(blockNumber int, predict *prediction) {
	for i := blockNumber + 1; i < blockNumber+11; i++ {
		_, ok := predict.scores[i]
		if !ok {
			targetPrediction, targetPredictionOk := s.predictions[i]
			if !targetPredictionOk {
				//target prediction does not yet exist
				continue
			}

			scoreEconomical := s.getPercentageOfTxsWithHigherFeeRate(targetPrediction.feeRates.Rates, predict.economicalFeeRate)
			scoreStandard := s.getPercentageOfTxsWithHigherFeeRate(targetPrediction.feeRates.Rates, predict.standardFeeRate)
			scoreFast := s.getPercentageOfTxsWithHigherFeeRate(targetPrediction.feeRates.Rates, predict.fastFeeRate)
			predict.scores[i] = &score{
				ScoreEconomical: scoreEconomical,
				ScoreStandard:   scoreStandard,
				ScoreFast:       scoreFast,
				NumberOfTxs:     targetPrediction.feeRates.NumberOfTxs,
			}
		}
	}
}

func (s *scores) getPercentageOfTxsWithHigherFeeRate(feeRates []int, prediction float64) float64 {
	sort.Ints(feeRates)
	for idx, feeRate := range feeRates {
		if float64(feeRate) > prediction {
			percentage := (1.0 - (float64(idx) / float64(len(feeRates)))) * 100.0 //(1-idx/txs)*100
			return percentage
		}
	}

	return 0
}
