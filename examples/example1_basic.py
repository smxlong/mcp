#!/usr/bin/env python3
"""
Example 1: Basic Batch Operations

This example demonstrates:
- Creating a tree with initial data
- Using batch operations to set multiple values
- Querying data
- Listing and deleting trees

Command-line options:
  --no-clean    Preserve trees after completion (leaves persisted data)
"""

import sys
import os

# Add parent directory to path for imports
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result, parse_example_args


def main():
    """Run the basic batch operations example."""
    
    args = parse_example_args("Example 1: Basic Batch Operations")
    
    print_header("Example 1: Basic Batch Operations")
    
    # Set up output directory for this example
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example1_basic'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Create a tree with initial data
        print_header("Step 1: Create Tree with Initial Data")
        
        initial_data = {
            "project": {
                "name": "Example Project",
                "version": "1.0.0",
                "status": "active"
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="project",
            data=initial_data
        )
        result_data = client.extract_result(result)
        print_result("Created tree 'project'", result_data)
        
        # Step 2: Use batch operations to add more data
        print_header("Step 2: Batch Set Multiple Values")
        
        batch_ops = [
            {
                "operation": "set",
                "tree": "project",
                "path": ".project.author",
                "value": "Jane Doe"
            },
            {
                "operation": "set",
                "tree": "project",
                "path": ".project.tags",
                "value": ["python", "example", "mcp"]
            },
            {
                "operation": "set",
                "tree": "project",
                "path": ".project.metadata",
                "value": {
                    "created": "2024-01-15",
                    "updated": "2024-01-15",
                    "license": "MIT"
                }
            }
        ]
        
        batch_result = client.call_batch(batch_ops)
        batch_data = client.extract_result(batch_result)
        print_result("Batch operation results", batch_data)
        
        # Step 3: Query the complete tree
        print_header("Step 3: Query Complete Tree")
        
        get_result = client.call_tool(
            operation="get",
            tree="project",
            path="."
        )
        get_data = client.extract_result(get_result)
        print_result("Complete tree data", get_data)
        
        # Step 4: Query specific paths with jq filters
        print_header("Step 4: Query Specific Paths with jq Filters")
        
        query_ops = [
            {
                "operation": "query",
                "tree": "project",
                "filter": ".project.name"
            },
            {
                "operation": "query",
                "tree": "project",
                "filter": ".project.tags[]"
            },
            {
                "operation": "query",
                "tree": "project",
                "filter": ".project.metadata.license"
            }
        ]
        
        query_result = client.call_batch(query_ops)
        query_data = client.extract_result(query_result)
        print_result("Query results", query_data)
        
        # Step 5: List all trees
        print_header("Step 5: List All Trees")
        
        list_result = client.call_tool(operation="list_trees")
        list_data = client.extract_result(list_result)
        print_result("Available trees", list_data)
        
        # Step 6: Clean up - delete the tree
        if not args.no_clean:
            print_header("Step 6: Clean Up")
            
            delete_result = client.call_tool(
                operation="delete_tree",
                tree="project"
            )
            delete_data = client.extract_result(delete_result)
            print_result("Deleted tree 'project'", delete_data)
        else:
            print_header("Step 6: Preservation")
            print(f"✓ Tree 'project' preserved in {output_dir}/trees/project.json")
        
        print_header("Example Complete!")
        print("✓ Demonstrated tree creation")
        print("✓ Demonstrated batch operations")
        print("✓ Demonstrated queries")
        if not args.no_clean:
            print("✓ Demonstrated cleanup")
        else:
            print("✓ Preserved tree data for inspection")


if __name__ == "__main__":
    main()
