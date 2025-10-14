#!/usr/bin/env python3
"""
Example 3: Time-Series Data and Window Management

This example demonstrates:
- Efficient time-series data structures
- Windowed append/prepend operations for bounded memory
- Time-based queries and aggregations
- Rolling calculations and trend analysis
- Multi-metric monitoring with batch operations

Command-line options:
  --no-clean    Preserve trees after completion (leaves persisted data)
"""

import sys
import os
import json
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result, parse_example_args


def generate_metrics(base_time, count, metric_name, base_value, variance):
    """Generate sample time-series metrics."""
    import random
    metrics = []
    for i in range(count):
        timestamp = (base_time + timedelta(minutes=i)).isoformat()
        value = base_value + random.uniform(-variance, variance)
        metrics.append({
            "timestamp": timestamp,
            "metric": metric_name,
            "value": round(value, 2),
            "tags": {"environment": "production", "host": f"server-{i % 3 + 1}"}
        })
    return metrics


def main():
    """Run the time-series data example."""
    
    args = parse_example_args("Example 3: Time-Series Data & Window Management")
    
    print_header("Example 3: Time-Series Data & Window Management")
    
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example3_timeseries'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Create time-series tree with windowed arrays
        print_header("Step 1: Initialize Time-Series Structure")
        
        initial_structure = {
            "metrics": {
                "cpu": [],
                "memory": [],
                "network": [],
                "disk": []
            },
            "metadata": {
                "window_size": 100,
                "retention": "Last 100 samples per metric",
                "update_frequency": "1 minute"
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="monitoring",
            data=initial_structure
        )
        print_result("Created monitoring tree", client.extract_result(result))
        
        # Step 2: Simulate real-time metric ingestion with windowing
        print_header("Step 2: Ingest Metrics with Window Management")
        
        base_time = datetime.now() - timedelta(hours=2)
        
        # Generate metrics
        cpu_metrics = generate_metrics(base_time, 50, "cpu", 45.0, 15.0)
        memory_metrics = generate_metrics(base_time, 50, "memory", 65.0, 10.0)
        
        print(f"Generating 50 CPU and 50 memory samples...")
        print(f"Using window_size=100 to automatically limit array length")
        print(f"Processing in batches of 30 operations (max 32 per batch)")
        
        # Batch append with window management (15 CPU + 15 memory = 30 per batch)
        batch_ops = []
        for metric in cpu_metrics[:15]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.cpu",
                "value": metric,
                "window": 100  # Keep only last 100 samples
            })
        
        for metric in memory_metrics[:15]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.memory",
                "value": metric,
                "window": 100
            })
        
        result = client.call_batch(batch_ops)
        data = client.extract_result(result)
        print_result("First batch ingestion (30 ops)", data)
        
        # Second batch
        batch_ops = []
        for metric in cpu_metrics[15:30]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.cpu",
                "value": metric,
                "window": 100
            })
        
        for metric in memory_metrics[15:30]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.memory",
                "value": metric,
                "window": 100
            })
        
        result = client.call_batch(batch_ops)
        data = client.extract_result(result)
        print_result("Second batch ingestion (30 ops)", data)
        
        # Third batch (remaining 20+20=40, split into 30)
        batch_ops = []
        for metric in cpu_metrics[30:45]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.cpu",
                "value": metric,
                "window": 100
            })
        
        for metric in memory_metrics[30:45]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.memory",
                "value": metric,
                "window": 100
            })
        
        result = client.call_batch(batch_ops)
        data = client.extract_result(result)
        print_result("Third batch ingestion (30 ops)", data)
        
        # Fourth batch (final 5+5=10)
        batch_ops = []
        for metric in cpu_metrics[45:]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.cpu",
                "value": metric,
                "window": 100
            })
        
        for metric in memory_metrics[45:]:
            batch_ops.append({
                "operation": "append",
                "tree": "monitoring",
                "path": ".metrics.memory",
                "value": metric,
                "window": 100
            })
        
        result = client.call_batch(batch_ops)
        data = client.extract_result(result)
        print_result("Fourth batch ingestion (10 ops)", data)
        
        # Step 3: Time-based queries
        print_header("Step 3: Time-Based Queries and Filtering")
        
        queries = [
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": ".metrics.cpu | length",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": ".metrics.cpu | map(.value) | add / length",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": ".metrics.cpu | map(.value) | max",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": ".metrics.cpu | map(.value) | min",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(queries)
        batch_data = client.extract_result(result)
        
        print("CPU Metrics Analysis:")
        results = batch_data.get("batch_results", [])
        if len(results) >= 4:
            print(f"  Sample count: {results[0].get('data')}")
            print(f"  Average: {results[1].get('data'):.2f}%")
            print(f"  Peak: {results[2].get('data'):.2f}%")
            print(f"  Minimum: {results[3].get('data'):.2f}%")
        
        # Step 4: Aggregate by host
        print_header("Step 4: Group and Aggregate by Tags")
        
        group_query = {
            "operation": "query",
            "tree": "monitoring",
            "filter": """.metrics.cpu | 
                group_by(.tags.host) | 
                map({
                    host: .[0].tags.host,
                    avg: (map(.value) | add / length),
                    count: length
                })""",
            "verbatim": True
        }
        
        result = client.call_tool(**group_query)
        data = client.extract_result(result)
        print_result("CPU usage by host", data.get("result"))
        
        # Step 5: Calculate rolling statistics
        print_header("Step 5: Calculate Rolling Statistics")
        
        # Use transform to add rolling averages
        result = client.call_tool(
            operation="transform",
            tree="monitoring",
            path=".analysis.cpu_last_10_avg",
            source_path=".metrics.cpu",
            filter=".[- 10:] | map(.value) | add / length"
        )
        print_result("Calculated last 10 samples average", client.extract_result(result))
        
        # Calculate trend (simple linear regression slope indication)
        result = client.call_tool(
            operation="transform",
            tree="monitoring",
            path=".analysis.trend_indicators",
            source_path=".metrics",
            filter="""
                {
                    cpu_recent_avg: (.cpu[-10:] | map(.value) | add / length),
                    cpu_older_avg: (.cpu[-20:-10] | map(.value) | add / length),
                    memory_recent_avg: (.memory[-10:] | map(.value) | add / length),
                    memory_older_avg: (.memory[-20:-10] | map(.value) | add / length)
                } | 
                . + {
                    cpu_trend: (if .cpu_recent_avg > .cpu_older_avg then "increasing" else "decreasing" end),
                    memory_trend: (if .memory_recent_avg > .memory_older_avg then "increasing" else "decreasing" end)
                }
            """
        )
        print_result("Trend analysis", client.extract_result(result))
        
        # Query the analysis
        result = client.call_tool(
            operation="get",
            tree="monitoring",
            path=".analysis"
        )
        data = client.extract_result(result)
        print_result("Complete analysis results", data.get("data"))
        
        # Step 6: Simulate alert conditions
        print_header("Step 6: Alert Condition Detection")
        
        alert_queries = [
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": ".metrics.cpu | map(select(.value > 70)) | length",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": """.metrics.cpu | 
                    map(select(.value > 70)) | 
                    group_by(.tags.host) | 
                    map({host: .[0].tags.host, violations: length})""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "monitoring",
                "filter": """.metrics | 
                    {
                        cpu_high: (.cpu | map(select(.value > 70)) | length),
                        memory_high: (.memory | map(select(.value > 80)) | length)
                    }""",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(alert_queries)
        batch_data = client.extract_result(result)
        
        print("Alert Analysis:")
        for i, alert_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\nAlert Query {i+1}:")
            print(json.dumps(alert_result.get("data"), indent=2))
        
        # Step 7: Demonstrate prepend for reverse chronological order
        print_header("Step 7: Reverse Chronological Events (Prepend)")
        
        # Add an events log using prepend for most-recent-first
        events = [
            {"timestamp": datetime.now().isoformat(), "level": "info", "message": "System healthy"},
            {"timestamp": (datetime.now() - timedelta(minutes=5)).isoformat(), "level": "warning", "message": "CPU spike detected"},
            {"timestamp": (datetime.now() - timedelta(minutes=10)).isoformat(), "level": "info", "message": "Monitoring started"}
        ]
        
        batch_ops = [
            {
                "operation": "set",
                "tree": "monitoring",
                "path": ".events",
                "value": []
            }
        ]
        
        for event in reversed(events):  # Add in reverse so prepend puts them in correct order
            batch_ops.append({
                "operation": "prepend",
                "tree": "monitoring",
                "path": ".events",
                "value": event,
                "window": 50  # Keep last 50 events
            })
        
        result = client.call_batch(batch_ops)
        print_result("Added events", client.extract_result(result))
        
        # Query events (most recent first)
        result = client.call_tool(
            operation="query",
            tree="monitoring",
            filter=".events",
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Event log (most recent first)", data.get("result"))
        
        # Clean up
        print_header("Complete!")
        if not args.no_clean:
            result = client.call_tool(operation="delete_tree", tree="monitoring")
            print_result("Cleaned up", client.extract_result(result))
        else:
            print(f"✓ Tree 'monitoring' preserved in {output_dir}/trees/monitoring.json")
        
        print("\n" + "="*60)
        print("Key Concepts Demonstrated:")
        print("="*60)
        print("✓ Time-series data structures with bounded memory")
        print("✓ Window management with append/prepend operations")
        print("✓ Batch ingestion for high-throughput scenarios")
        print("✓ Statistical aggregations (avg, min, max)")
        print("✓ Group-by operations on nested tags")
        print("✓ Rolling calculations and trend analysis")
        print("✓ Alert condition detection with filtering")
        print("✓ Reverse chronological ordering with prepend")
        print("✓ Transform operations for derived metrics")


if __name__ == "__main__":
    main()
