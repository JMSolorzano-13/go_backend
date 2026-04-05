#!/bin/bash
# Test script to generate 5 SAT requests for company 6450eba4-4715-4181-9a1a-7949f9c8cf1f
# Date range: 2026-01-01 to 2026-03-31 (Q1 2026)

cd "$(dirname "$0")"

echo "=== Testing SAT Request Generator ==="
echo "Company: 6450eba4-4715-4181-9a1a-7949f9c8cf1f"
echo "Date range: 2026-01-01 to 2026-03-31"
echo ""

# Provide inputs: company UUID, start date, end date, confirm yes
echo -e "6450eba4-4715-4181-9a1a-7949f9c8cf1f\n2026-01-01\n2026-03-31\nyes" | ./bin/sat-request-generator

echo ""
echo "=== Test Complete ==="
