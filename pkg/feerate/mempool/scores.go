package mempool

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
	ScoreStandard float64
	NumberOfTxs   int
}

type rate struct {
	predictedRate float64
	scores        map[int]*score
}

type prediction struct {
	feeRates       *feerate.FeeRates
	height         int
	predictedRates []rate
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

func (s *scores) addPrediction(height int, rates *feerate.FeeRates, predictedRate float64) {
	_, ok := s.predictions[height]
	if !ok {
		s.predictions[height] = &prediction{
			height:         height,
			feeRates:       rates,
			predictedRates: []rate{rate{predictedRate: predictedRate, scores: make(map[int]*score)}},
		}
	} else {
		s.predictions[height].predictedRates = append(s.predictions[height].predictedRates, rate{predictedRate: predictedRate, scores: make(map[int]*score)})
	}
}

func (s *scores) predictScores() error {
	for num, pred := range s.predictions {
		s.comparePredictionToNext10Blocks(num, pred)
	}

	return s.flush()
}

func (s *scores) flush() error {
	fileName := fmt.Sprintf("mempoolscores%v.csv", time.Now().Format(time.RFC3339))
	f, err := os.OpenFile("./output/"+fileName, os.O_CREATE|os.O_RDWR, 0660)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	err = w.Write([]string{
		"block_number",
		"priceStandard",
		"numberOfTxs",
		"scoreStandardPlus1",
		"scoreStandardPlus2",
		"scoreStandardPlus3",
		"scoreStandardPlus4",
		"scoreStandardPlus5",
		"scoreStandardPlus6",
		"scoreStandardPlus7",
		"scoreStandardPlus8",
		"scoreStandardPlus9",
		"scoreStandardPlus10",
	})

	if err != nil {
		return err
	}

	var records [][]string
	for blockHeight, prediction := range s.predictions {
		for _, rate := range prediction.predictedRates {
			record := []string{
				strconv.Itoa(blockHeight),
				strconv.FormatFloat(rate.predictedRate, 'f', 3, 64),
				strconv.Itoa(prediction.feeRates.NumberOfTxs),
			}
			for i := blockHeight + 1; i < blockHeight+11; i++ {
				score, ok := rate.scores[i]
				if !ok {
					record = append(record, strconv.Itoa(-1))
				} else {
					record = append(record, strconv.FormatFloat(score.ScoreStandard, 'f', 3, 64))
				}
			}

			records = append(records, record)
		}
	}

	s.logger.Info("prediction score", zap.Any("scores", records))
	return w.WriteAll(records)
}

func (s *scores) comparePredictionToNext10Blocks(blockNumber int, predict *prediction) {
	for i := blockNumber + 1; i < blockNumber+11; i++ {
		for _, rate := range predict.predictedRates {
			_, ok := rate.scores[i]
			if !ok {
				targetPrediction, targetPredictionOk := s.predictions[i]
				if !targetPredictionOk {
					//target prediction does not yet exist
					continue
				}

				for _, rate := range predict.predictedRates {
					scoreStandard := s.getPercentageOfTxsWithHigherFeeRate(targetPrediction.feeRates.Rates, rate.predictedRate)
					rate.scores[i] = &score{
						ScoreStandard: scoreStandard,
						NumberOfTxs:   targetPrediction.feeRates.NumberOfTxs,
					}
				}
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
