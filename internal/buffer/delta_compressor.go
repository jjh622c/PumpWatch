package buffer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"

	"PumpWatch/internal/models"
)

// DeltaCompressor는 거래 데이터를 효율적으로 압축
type DeltaCompressor struct {
	basePrice  float64 // 기준 가격
	baseTime   int64   // 기준 시간
	priceScale int     // 가격 스케일 (소수점 처리)
}

// CompressedData represents the compressed trade data structure
type CompressedData struct {
	BasePrice   float64  // 기준 가격
	BaseTime    int64    // 기준 시간
	PriceScale  int      // 가격 스케일
	Count       int      // 거래 수
	PriceDeltas []int16  // 16비트 가격 차분
	TimeDeltas  []uint16 // 16비트 시간 차분 (ms)
	Volumes     []uint32 // 32비트 볼륨 (압축)
	Symbols     []byte   // 심볼 압축 데이터
	Exchanges   []byte   // 거래소 압축 데이터
	MarketTypes []byte   // 마켓 타입 압축 데이터
	Sides       []byte   // 거래 방향 압축 데이터 (buy=0, sell=1)
}

// SymbolMapping represents compressed symbol information
type SymbolMapping struct {
	UniqueSymbols []string
	SymbolMap     map[string]uint8 // symbol -> index mapping
}

// NewDeltaCompressor creates a new delta compressor
func NewDeltaCompressor() *DeltaCompressor {
	return &DeltaCompressor{
		priceScale: 1000000, // 6 decimal places
	}
}

// Compress compresses an array of trade events using delta encoding
func (dc *DeltaCompressor) Compress(trades []models.TradeEvent) ([]byte, error) {
	if len(trades) == 0 {
		return nil, fmt.Errorf("no trades to compress")
	}

	// Sort trades by timestamp for better compression
	sortedTrades := make([]models.TradeEvent, len(trades))
	copy(sortedTrades, trades)
	sort.Slice(sortedTrades, func(i, j int) bool {
		return sortedTrades[i].Timestamp < sortedTrades[j].Timestamp
	})

	// Set base values from first trade
	basePrice, err := strconv.ParseFloat(sortedTrades[0].Price, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse base price: %w", err)
	}
	dc.basePrice = basePrice
	dc.baseTime = sortedTrades[0].Timestamp

	// Create compressed data structure
	compressed := CompressedData{
		BasePrice:   dc.basePrice,
		BaseTime:    dc.baseTime,
		PriceScale:  dc.priceScale,
		Count:       len(sortedTrades),
		PriceDeltas: make([]int16, len(sortedTrades)),
		TimeDeltas:  make([]uint16, len(sortedTrades)),
		Volumes:     make([]uint32, len(sortedTrades)),
	}

	// Create symbol mapping for compression
	symbolMapping := dc.createSymbolMapping(sortedTrades)

	// Compress each trade
	for i, trade := range sortedTrades {
		// Parse price and quantity
		tradePrice, err := strconv.ParseFloat(trade.Price, 64)
		if err != nil {
			continue // Skip invalid prices
		}

		tradeQuantity, err := strconv.ParseFloat(trade.Quantity, 64)
		if err != nil {
			continue // Skip invalid quantities
		}

		// Price delta compression
		priceDelta := (tradePrice - dc.basePrice) * float64(dc.priceScale)
		if priceDelta > math.MaxInt16 || priceDelta < math.MinInt16 {
			// If delta is too large, adjust base price
			dc.basePrice = tradePrice
			compressed.BasePrice = dc.basePrice
			priceDelta = 0
		}
		compressed.PriceDeltas[i] = int16(priceDelta)

		// Time delta compression (in milliseconds)
		timeDelta := (trade.Timestamp - dc.baseTime) / 1000000 // ns to ms
		if timeDelta > math.MaxUint16 {
			// If time delta is too large, adjust base time
			dc.baseTime = trade.Timestamp
			compressed.BaseTime = dc.baseTime
			timeDelta = 0
		}
		compressed.TimeDeltas[i] = uint16(timeDelta)

		// Volume compression (convert to microunits)
		volumeCompressed := uint32(tradeQuantity * 1000000) // 6 decimal places
		compressed.Volumes[i] = volumeCompressed
	}

	// Compress categorical data
	compressed.Symbols = dc.compressSymbols(sortedTrades, symbolMapping)
	compressed.Exchanges = dc.compressExchanges(sortedTrades)
	compressed.MarketTypes = dc.compressMarketTypes(sortedTrades)
	compressed.Sides = dc.compressSides(sortedTrades)

	// Serialize to binary format
	return dc.serializeToBinary(compressed, symbolMapping)
}

// Decompress decompresses binary data back to trade events
func (dc *DeltaCompressor) Decompress(data []byte) ([]models.TradeEvent, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Deserialize from binary format
	compressed, symbolMapping, err := dc.deserializeFromBinary(data)
	if err != nil {
		return nil, fmt.Errorf("deserialization failed: %w", err)
	}

	// Reconstruct trade events
	trades := make([]models.TradeEvent, compressed.Count)

	for i := 0; i < compressed.Count; i++ {
		trade := models.TradeEvent{}

		// Reconstruct price
		priceDelta := float64(compressed.PriceDeltas[i]) / float64(compressed.PriceScale)
		trade.Price = strconv.FormatFloat(compressed.BasePrice + priceDelta, 'f', -1, 64)

		// Reconstruct timestamp
		timeDelta := int64(compressed.TimeDeltas[i]) * 1000000 // ms to ns
		trade.Timestamp = compressed.BaseTime + timeDelta

		// Reconstruct quantity
		trade.Quantity = strconv.FormatFloat(float64(compressed.Volumes[i]) / 1000000, 'f', -1, 64)

		// Reconstruct categorical data
		trade.Symbol = dc.decompressSymbol(compressed.Symbols, i, symbolMapping)
		trade.Exchange = dc.decompressExchange(compressed.Exchanges, i)
		trade.MarketType = dc.decompressMarketType(compressed.MarketTypes, i)
		trade.Side = dc.decompressSide(compressed.Sides, i)

		trades[i] = trade
	}

	return trades, nil
}

// createSymbolMapping creates a mapping of unique symbols to indices
func (dc *DeltaCompressor) createSymbolMapping(trades []models.TradeEvent) SymbolMapping {
	uniqueSymbols := make(map[string]bool)
	for _, trade := range trades {
		uniqueSymbols[trade.Symbol] = true
	}

	mapping := SymbolMapping{
		UniqueSymbols: make([]string, 0, len(uniqueSymbols)),
		SymbolMap:     make(map[string]uint8),
	}

	i := uint8(0)
	for symbol := range uniqueSymbols {
		mapping.UniqueSymbols = append(mapping.UniqueSymbols, symbol)
		mapping.SymbolMap[symbol] = i
		i++
	}

	return mapping
}

// compressSymbols compresses symbol data using mapping
func (dc *DeltaCompressor) compressSymbols(trades []models.TradeEvent, mapping SymbolMapping) []byte {
	symbolIndices := make([]uint8, len(trades))
	for i, trade := range trades {
		symbolIndices[i] = mapping.SymbolMap[trade.Symbol]
	}
	return symbolIndices
}

// compressExchanges compresses exchange names
func (dc *DeltaCompressor) compressExchanges(trades []models.TradeEvent) []byte {
	exchangeMap := map[string]uint8{
		"binance": 0, "bybit": 1, "okx": 2, "kucoin": 3, "gate": 4, "phemex": 5,
	}

	exchangeIndices := make([]uint8, len(trades))
	for i, trade := range trades {
		if idx, exists := exchangeMap[trade.Exchange]; exists {
			exchangeIndices[i] = idx
		}
	}
	return exchangeIndices
}

// compressMarketTypes compresses market type information
func (dc *DeltaCompressor) compressMarketTypes(trades []models.TradeEvent) []byte {
	marketMap := map[string]uint8{
		"spot": 0, "futures": 1, "margin": 2,
	}

	marketIndices := make([]uint8, len(trades))
	for i, trade := range trades {
		if idx, exists := marketMap[trade.MarketType]; exists {
			marketIndices[i] = idx
		}
	}
	return marketIndices
}

// compressSides compresses trade side information (buy/sell)
func (dc *DeltaCompressor) compressSides(trades []models.TradeEvent) []byte {
	// Use bit packing: 8 trades per byte
	numBytes := (len(trades) + 7) / 8
	sideData := make([]byte, numBytes)

	for i, trade := range trades {
		byteIndex := i / 8
		bitIndex := uint(i % 8)

		if trade.Side == "sell" {
			sideData[byteIndex] |= (1 << bitIndex)
		}
		// buy = 0 (default), sell = 1
	}

	return sideData
}

// serializeToBinary serializes compressed data to binary format
func (dc *DeltaCompressor) serializeToBinary(compressed CompressedData, mapping SymbolMapping) ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write header
	binary.Write(buf, binary.LittleEndian, compressed.BasePrice)
	binary.Write(buf, binary.LittleEndian, compressed.BaseTime)
	binary.Write(buf, binary.LittleEndian, int32(compressed.PriceScale))
	binary.Write(buf, binary.LittleEndian, int32(compressed.Count))

	// Write symbol mapping
	binary.Write(buf, binary.LittleEndian, int32(len(mapping.UniqueSymbols)))
	for _, symbol := range mapping.UniqueSymbols {
		symbolBytes := []byte(symbol)
		binary.Write(buf, binary.LittleEndian, int32(len(symbolBytes)))
		buf.Write(symbolBytes)
	}

	// Write compressed arrays
	dc.writeInt16Array(buf, compressed.PriceDeltas)
	dc.writeUint16Array(buf, compressed.TimeDeltas)
	dc.writeUint32Array(buf, compressed.Volumes)

	// Write categorical data
	dc.writeByteArray(buf, compressed.Symbols)
	dc.writeByteArray(buf, compressed.Exchanges)
	dc.writeByteArray(buf, compressed.MarketTypes)
	dc.writeByteArray(buf, compressed.Sides)

	return buf.Bytes(), nil
}

// deserializeFromBinary deserializes binary data to compressed structure
func (dc *DeltaCompressor) deserializeFromBinary(data []byte) (CompressedData, SymbolMapping, error) {
	buf := bytes.NewReader(data)
	var compressed CompressedData
	var mapping SymbolMapping

	// Read header
	binary.Read(buf, binary.LittleEndian, &compressed.BasePrice)
	binary.Read(buf, binary.LittleEndian, &compressed.BaseTime)
	var priceScale, count int32
	binary.Read(buf, binary.LittleEndian, &priceScale)
	binary.Read(buf, binary.LittleEndian, &count)
	compressed.PriceScale = int(priceScale)
	compressed.Count = int(count)

	// Read symbol mapping
	var symbolCount int32
	binary.Read(buf, binary.LittleEndian, &symbolCount)
	mapping.UniqueSymbols = make([]string, symbolCount)
	mapping.SymbolMap = make(map[string]uint8)

	for i := int32(0); i < symbolCount; i++ {
		var symbolLen int32
		binary.Read(buf, binary.LittleEndian, &symbolLen)
		symbolBytes := make([]byte, symbolLen)
		buf.Read(symbolBytes)
		symbol := string(symbolBytes)
		mapping.UniqueSymbols[i] = symbol
		mapping.SymbolMap[symbol] = uint8(i)
	}

	// Read compressed arrays
	compressed.PriceDeltas = dc.readInt16Array(buf, compressed.Count)
	compressed.TimeDeltas = dc.readUint16Array(buf, compressed.Count)
	compressed.Volumes = dc.readUint32Array(buf, compressed.Count)

	// Read categorical data
	compressed.Symbols = dc.readByteArray(buf, compressed.Count)
	compressed.Exchanges = dc.readByteArray(buf, compressed.Count)
	compressed.MarketTypes = dc.readByteArray(buf, compressed.Count)
	sideBytesCount := (compressed.Count + 7) / 8
	compressed.Sides = dc.readByteArray(buf, sideBytesCount)

	return compressed, mapping, nil
}

// Helper methods for binary serialization
func (dc *DeltaCompressor) writeInt16Array(buf *bytes.Buffer, arr []int16) {
	binary.Write(buf, binary.LittleEndian, int32(len(arr)))
	for _, val := range arr {
		binary.Write(buf, binary.LittleEndian, val)
	}
}

func (dc *DeltaCompressor) writeUint16Array(buf *bytes.Buffer, arr []uint16) {
	binary.Write(buf, binary.LittleEndian, int32(len(arr)))
	for _, val := range arr {
		binary.Write(buf, binary.LittleEndian, val)
	}
}

func (dc *DeltaCompressor) writeUint32Array(buf *bytes.Buffer, arr []uint32) {
	binary.Write(buf, binary.LittleEndian, int32(len(arr)))
	for _, val := range arr {
		binary.Write(buf, binary.LittleEndian, val)
	}
}

func (dc *DeltaCompressor) writeByteArray(buf *bytes.Buffer, arr []byte) {
	binary.Write(buf, binary.LittleEndian, int32(len(arr)))
	buf.Write(arr)
}

func (dc *DeltaCompressor) readInt16Array(buf *bytes.Reader, count int) []int16 {
	var length int32
	binary.Read(buf, binary.LittleEndian, &length)
	arr := make([]int16, length)
	for i := int32(0); i < length; i++ {
		binary.Read(buf, binary.LittleEndian, &arr[i])
	}
	return arr
}

func (dc *DeltaCompressor) readUint16Array(buf *bytes.Reader, count int) []uint16 {
	var length int32
	binary.Read(buf, binary.LittleEndian, &length)
	arr := make([]uint16, length)
	for i := int32(0); i < length; i++ {
		binary.Read(buf, binary.LittleEndian, &arr[i])
	}
	return arr
}

func (dc *DeltaCompressor) readUint32Array(buf *bytes.Reader, count int) []uint32 {
	var length int32
	binary.Read(buf, binary.LittleEndian, &length)
	arr := make([]uint32, length)
	for i := int32(0); i < length; i++ {
		binary.Read(buf, binary.LittleEndian, &arr[i])
	}
	return arr
}

func (dc *DeltaCompressor) readByteArray(buf *bytes.Reader, maxCount int) []byte {
	var length int32
	binary.Read(buf, binary.LittleEndian, &length)
	arr := make([]byte, length)
	buf.Read(arr)
	return arr
}

// Decompression helper methods
func (dc *DeltaCompressor) decompressSymbol(symbolData []byte, index int, mapping SymbolMapping) string {
	if index < len(symbolData) {
		symbolIndex := symbolData[index]
		if int(symbolIndex) < len(mapping.UniqueSymbols) {
			return mapping.UniqueSymbols[symbolIndex]
		}
	}
	return ""
}

func (dc *DeltaCompressor) decompressExchange(exchangeData []byte, index int) string {
	exchangeNames := []string{"binance", "bybit", "okx", "kucoin", "gate", "phemex"}
	if index < len(exchangeData) {
		exchangeIndex := exchangeData[index]
		if int(exchangeIndex) < len(exchangeNames) {
			return exchangeNames[exchangeIndex]
		}
	}
	return ""
}

func (dc *DeltaCompressor) decompressMarketType(marketData []byte, index int) string {
	marketNames := []string{"spot", "futures", "margin"}
	if index < len(marketData) {
		marketIndex := marketData[index]
		if int(marketIndex) < len(marketNames) {
			return marketNames[marketIndex]
		}
	}
	return ""
}

func (dc *DeltaCompressor) decompressSide(sideData []byte, index int) string {
	byteIndex := index / 8
	bitIndex := uint(index % 8)

	if byteIndex < len(sideData) {
		bit := (sideData[byteIndex] >> bitIndex) & 1
		if bit == 1 {
			return "sell"
		}
	}
	return "buy"
}