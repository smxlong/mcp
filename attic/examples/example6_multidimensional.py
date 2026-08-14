#!/usr/bin/env python3
"""
Example 6: Multi-Dimensional Analysis and OLAP-style Queries

This example demonstrates:
- Cube/dimension modeling for analytics
- Slice and dice operations
- Roll-up and drill-down aggregations
- Pivot operations
- Multi-dimensional filtering
- Advanced grouping and cross-tabulation

Command-line options:
  --no-clean    Preserve trees after completion (leaves persisted data)
"""

import sys
import os
import json
from datetime import datetime, timedelta
import random

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result, parse_example_args


def generate_sales_data(count=50):
    """Generate sample multi-dimensional sales data."""
    products = ["Laptop", "Mouse", "Keyboard", "Monitor", "Headphones"]
    regions = ["North", "South", "East", "West"]
    channels = ["Online", "Retail", "Partner"]
    
    sales = []
    base_date = datetime.now() - timedelta(days=90)
    
    for i in range(count):
        date = (base_date + timedelta(days=random.randint(0, 90))).date().isoformat()
        product = random.choice(products)
        region = random.choice(regions)
        channel = random.choice(channels)
        
        # Base prices
        prices = {"Laptop": 1200, "Mouse": 25, "Keyboard": 75, "Monitor": 400, "Headphones": 150}
        base_price = prices[product]
        quantity = random.randint(1, 10)
        revenue = base_price * quantity * random.uniform(0.9, 1.1)
        
        sales.append({
            "sale_id": f"SALE{i:04d}",
            "date": date,
            "product": product,
            "region": region,
            "channel": channel,
            "quantity": quantity,
            "revenue": round(revenue, 2),
            "cost": round(revenue * 0.6, 2)
        })
    
    return sales


def main():
    """Run the multi-dimensional analysis example."""
    
    args = parse_example_args("Example 6: Multi-Dimensional Analysis (OLAP-style)")
    
    print_header("Example 6: Multi-Dimensional Analysis (OLAP-style)")
    
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example6_multidimensional'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Create data cube structure
        print_header("Step 1: Initialize Multi-Dimensional Data Cube")
        
        initial_structure = {
            "facts": [],  # Fact table
            "dimensions": {
                "products": ["Laptop", "Mouse", "Keyboard", "Monitor", "Headphones"],
                "regions": ["North", "South", "East", "West"],
                "channels": ["Online", "Retail", "Partner"],
                "time_periods": []  # Will be populated
            },
            "aggregations": {},
            "metadata": {
                "cube_name": "sales_cube",
                "measures": ["revenue", "cost", "quantity", "profit"],
                "dimensions": ["product", "region", "channel", "date"]
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="cube",
            data=initial_structure
        )
        print_result("Created data cube", client.extract_result(result))
        
        # Step 2: Load fact data in batches
        print_header("Step 2: Load Fact Data (Sales Transactions)")
        
        sales_data = generate_sales_data(60)
        print(f"Generated {len(sales_data)} sales transactions")
        
        # Load in batches of 30
        for batch_num in range(0, len(sales_data), 30):
            batch = sales_data[batch_num:batch_num + 30]
            batch_ops = []
            for sale in batch:
                batch_ops.append({
                    "operation": "append",
                    "tree": "cube",
                    "path": ".facts",
                    "value": sale
                })
            
            result = client.call_batch(batch_ops)
            data = client.extract_result(result)
            print(f"  Loaded batch {batch_num//30 + 1}: {data.get('successful_operations', 0)} records")
        
        # Step 3: Slice operation - filter by single dimension
        print_header("Step 3: Slice - Filter by Product")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts | 
                map(select(.product == "Laptop")) |
                {
                    product: "Laptop",
                    total_revenue: (map(.revenue) | add),
                    total_quantity: (map(.quantity) | add),
                    transaction_count: length,
                    average_sale: ((map(.revenue) | add) / length)
                }
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Laptop sales slice", data.get("result"))
        
        # Step 4: Dice operation - filter by multiple dimensions
        print_header("Step 4: Dice - Filter by Product AND Region")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts | 
                map(select(.product == "Monitor" and .region == "North")) |
                {
                    product: "Monitor",
                    region: "North",
                    total_revenue: (map(.revenue) | add),
                    sales_count: length
                }
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Monitor sales in North region", data.get("result"))
        
        # Step 5: Roll-up - aggregate to higher level
        print_header("Step 5: Roll-Up - Aggregate by Region")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts |
                group_by(.region) |
                map({
                    region: .[0].region,
                    total_revenue: (map(.revenue) | add),
                    total_cost: (map(.cost) | add),
                    profit: ((map(.revenue) | add) - (map(.cost) | add)),
                    transaction_count: length,
                    profit_margin: (((map(.revenue) | add) - (map(.cost) | add)) / (map(.revenue) | add) * 100)
                }) |
                sort_by(.total_revenue) |
                reverse
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Revenue by region (rolled up)", data.get("result"))
        
        # Step 6: Drill-down - go to more detailed level
        print_header("Step 6: Drill-Down - Region → Product breakdown")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts |
                group_by(.region) |
                map({
                    region: .[0].region,
                    products: (
                        group_by(.product) |
                        map({
                            product: .[0].product,
                            revenue: (map(.revenue) | add),
                            count: length
                        }) |
                        sort_by(.revenue) |
                        reverse |
                        .[0:3]
                    ),
                    total_revenue: (map(.revenue) | add)
                }) |
                sort_by(.total_revenue) |
                reverse
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Top 3 products per region (drilled down)", data.get("result"))
        
        # Step 7: Pivot - cross-tabulation
        print_header("Step 7: Pivot - Product vs Channel Matrix")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts |
                group_by(.product) |
                map({
                    product: .[0].product,
                    channels: (
                        group_by(.channel) |
                        map({
                            channel: .[0].channel,
                            revenue: (map(.revenue) | add)
                        })
                    )
                })
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Product × Channel pivot", data.get("result"))
        
        # Step 8: Multi-dimensional aggregation
        print_header("Step 8: Multi-Dimensional Cube Aggregation")
        
        result = client.call_tool(
            operation="transform",
            tree="cube",
            path=".aggregations.by_product_region",
            source_path=".facts",
            filter="""
                group_by(.product) |
                map({
                    (.[0].product): (
                        group_by(.region) |
                        map({
                            (.[0].region): {
                                revenue: (map(.revenue) | add),
                                quantity: (map(.quantity) | add),
                                transactions: length
                            }
                        }) |
                        add
                    )
                }) |
                add
            """
        )
        print_result("Built product×region aggregation", client.extract_result(result))
        
        # Query the aggregation
        result = client.call_tool(
            operation="get",
            tree="cube",
            path=".aggregations.by_product_region"
        )
        data = client.extract_result(result)
        print_result("Product×Region cube", data.get("data"))
        
        # Step 9: Time-series dimension analysis
        print_header("Step 9: Temporal Analysis")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts |
                group_by(.date) |
                map({
                    date: .[0].date,
                    daily_revenue: (map(.revenue) | add),
                    transactions: length
                }) |
                sort_by(.date) |
                .[-10:]
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Last 10 days revenue trend", data.get("result"))
        
        # Step 10: Advanced multi-dimensional query
        print_header("Step 10: Complex Multi-Dimensional Analysis")
        
        result = client.call_tool(
            operation="query",
            tree="cube",
            filter="""
                .facts |
                {
                    overall: {
                        total_revenue: (map(.revenue) | add),
                        total_profit: ((map(.revenue) | add) - (map(.cost) | add)),
                        transaction_count: length
                    },
                    by_channel: (
                        group_by(.channel) |
                        map({
                            channel: .[0].channel,
                            revenue: (map(.revenue) | add),
                            profit: ((map(.revenue) | add) - (map(.cost) | add)),
                            avg_transaction: ((map(.revenue) | add) / length)
                        })
                    ),
                    top_products: (
                        group_by(.product) |
                        map({
                            product: .[0].product,
                            revenue: (map(.revenue) | add)
                        }) |
                        sort_by(.revenue) |
                        reverse |
                        .[0:5]
                    ),
                    regional_performance: (
                        group_by(.region) |
                        map({
                            region: .[0].region,
                            revenue: (map(.revenue) | add),
                            profit_margin: (((map(.revenue) | add) - (map(.cost) | add)) / (map(.revenue) | add) * 100)
                        }) |
                        sort_by(.profit_margin) |
                        reverse
                    )
                }
            """,
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Comprehensive multi-dimensional analysis", data.get("result"))
        
        # Clean up
        print_header("Complete!")
        if not args.no_clean:
            result = client.call_tool(operation="delete_tree", tree="cube")
            print_result("Cleaned up", client.extract_result(result))
        else:
            print(f"✓ Tree 'cube' preserved in {output_dir}/trees/cube.json")
        
        print("\n" + "="*60)
        print("Key Concepts Demonstrated:")
        print("="*60)
        print("✓ Multi-dimensional data cube modeling")
        print("✓ Fact and dimension table structure")
        print("✓ Slice operations (single dimension filter)")
        print("✓ Dice operations (multi-dimension filter)")
        print("✓ Roll-up aggregations (higher level)")
        print("✓ Drill-down analysis (detailed level)")
        print("✓ Pivot operations (cross-tabulation)")
        print("✓ Pre-computed aggregations for performance")
        print("✓ Temporal dimension analysis")
        print("✓ Complex multi-measure analytics")


if __name__ == "__main__":
    main()
