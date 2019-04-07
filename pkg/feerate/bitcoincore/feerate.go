package bitcoincore

const satoshisInBtc = 100000000

type FeeRate struct {
	SatoshisPerK float64
}

func NewFeeRate(feePaidInBtc float64, transactionSizeInBytes int) *FeeRate {
	fee := &FeeRate{}
	if transactionSizeInBytes > 0 {
		btcPerByte := feePaidInBtc / float64(transactionSizeInBytes)
		btcPerK := btcPerByte * 1000
		fee.SatoshisPerK = btcPerK * satoshisInBtc //TODO is this conversion ok
	} else {
		fee.SatoshisPerK = 0
	}

	return fee
}

func FromSatoshisPerK(satoshisPerK float64) *FeeRate {
	fee := NewFeeRate(0, 0)
	fee.SatoshisPerK = satoshisPerK
	return fee
}

func (fr FeeRate) GetFee(transactionSizeInBytes int) float64 {
	feeInSatoshis := (fr.SatoshisPerK * float64(transactionSizeInBytes)) / 1000
	if feeInSatoshis == 0 && transactionSizeInBytes != 0 {
		if fr.SatoshisPerK > 0 {
			feeInSatoshis = 1
		}

		if fr.SatoshisPerK < 0 {
			feeInSatoshis = -1
		}
	}

	return feeInSatoshis
}

func (fr FeeRate) GetFeePerK() float64 {
	return fr.GetFee(1000)
}
