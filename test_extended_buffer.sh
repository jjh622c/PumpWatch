#!/bin/bash

# ExtendedBuffer 시스템 테스트 실행 스크립트

echo "🧪 ExtendedBuffer System Testing"
echo "================================="

echo ""
echo "📊 Running Memory Usage Test..."
echo "Expected: Memory usage < 4GB, Good compression ratio"
go test -v ./internal/buffer -run TestExtendedBufferMemoryUsage

echo ""
echo "⚡ Running Performance Tests..."
echo "Expected: Hot buffer < 1ms, Cold buffer < 10ms"
go test -v ./internal/buffer -run TestBufferPerformance

echo ""
echo "🗜️  Running Compression Test..."
echo "Expected: ~70% compression ratio with data integrity"
go test -v ./internal/buffer -run TestCompressionRatio

echo ""
echo "🔄 Running CollectionEvent Compatibility Test..."
echo "Expected: Seamless integration with legacy system"
go test -v ./internal/buffer -run TestCollectionEventCompatibility

echo ""
echo "🏎️  Running Benchmark Tests..."
echo "Measuring concurrent access performance..."
echo "Hot Buffer Access Benchmark:"
go test -v ./internal/buffer -bench BenchmarkHotBufferAccess -benchtime=5s

echo ""
echo "Cold Buffer Access Benchmark:"
go test -v ./internal/buffer -bench BenchmarkColdBufferAccess -benchtime=5s

echo ""
echo "📈 Memory Profile Test (if memory profiling is needed)..."
echo "Run with: go test -v ./internal/buffer -memprofile=mem.prof -run TestExtendedBufferMemoryUsage"
echo "Then analyze with: go tool pprof mem.prof"

echo ""
echo "✅ ExtendedBuffer Testing Complete!"
echo "======================================"
echo ""
echo "📋 Summary:"
echo "   - Memory usage should be < 4GB for 10-minute buffer"
echo "   - Hot buffer access should be < 1ms"
echo "   - Cold buffer access should be < 10ms"
echo "   - Compression should achieve ~70% ratio"
echo "   - Legacy CollectionEvent compatibility maintained"
echo ""
echo "🚀 If all tests pass, the ExtendedBuffer is ready for integration!"