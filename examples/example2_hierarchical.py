#!/usr/bin/env python3
"""
Example 2: Hierarchical Data Organization

This example demonstrates:
- Organizing data in hierarchical structures (projects, tasks, subtasks)
- Using jq filters to navigate and query hierarchies
- Batch operations for efficient tree construction
- Advanced filtering with arrays and nested objects
- Aggregation queries across hierarchy levels

Command-line options:
  --no-clean    Preserve trees after completion (leaves persisted data)
"""

import sys
import os

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mt_common import MTClient, print_header, print_result, parse_example_args


def main():
    """Run the hierarchical data organization example."""
    
    args = parse_example_args("Example 2: Hierarchical Data Organization")
    
    print_header("Example 2: Hierarchical Data Organization")
    
    output_dir = os.path.join(
        os.path.dirname(__file__), 
        'output', 
        'example2_hierarchical'
    )
    
    with MTClient(output_dir) as client:
        print("✓ Connected to MT server")
        print(f"✓ Data directory: {output_dir}")
        
        # Step 1: Create a project management tree
        print_header("Step 1: Create Project Hierarchy")
        
        initial_structure = {
            "workspace": {
                "name": "Development Team",
                "projects": []
            }
        }
        
        result = client.call_tool(
            operation="create_tree",
            tree="projects",
            data=initial_structure
        )
        print_result("Created projects tree", client.extract_result(result))
        
        # Step 2: Build hierarchy with batch operations using append
        print_header("Step 2: Build Complete Hierarchy in Batch")
        
        batch_ops = [
            # Add Project Alpha with all its tasks
            {
                "operation": "append",
                "tree": "projects",
                "path": ".workspace.projects",
                "value": {
                    "id": "proj-001",
                    "name": "Project Alpha",
                    "status": "active",
                    "priority": "high",
                    "team": ["alice", "bob", "charlie"],
                    "tasks": [
                        {
                            "id": "task-001",
                            "title": "Setup infrastructure",
                            "status": "completed",
                            "assignee": "alice",
                            "hours": 16,
                            "subtasks": [
                                {"id": "sub-001", "title": "Configure servers", "status": "completed"},
                                {"id": "sub-002", "title": "Setup database", "status": "completed"}
                            ]
                        },
                        {
                            "id": "task-002",
                            "title": "Develop API",
                            "status": "in_progress",
                            "assignee": "bob",
                            "hours": 24,
                            "subtasks": [
                                {"id": "sub-003", "title": "Design endpoints", "status": "completed"},
                                {"id": "sub-004", "title": "Implement handlers", "status": "in_progress"}
                            ]
                        },
                        {
                            "id": "task-003",
                            "title": "Write tests",
                            "status": "pending",
                            "assignee": "charlie",
                            "hours": 12,
                            "subtasks": []
                        }
                    ]
                }
            },
            # Add Project Beta
            {
                "operation": "append",
                "tree": "projects",
                "path": ".workspace.projects",
                "value": {
                    "id": "proj-002",
                    "name": "Project Beta",
                    "status": "planning",
                    "priority": "medium",
                    "team": ["alice", "diana"],
                    "tasks": [
                        {
                            "id": "task-004",
                            "title": "Requirements gathering",
                            "status": "in_progress",
                            "assignee": "diana",
                            "hours": 8,
                            "subtasks": []
                        }
                    ]
                }
            }
        ]
        
        result = client.call_batch(batch_ops)
        print_result("Built hierarchy", client.extract_result(result))
        
        # Step 3: Navigate the hierarchy with queries
        print_header("Step 3: Navigate Hierarchy with jq Queries")
        
        queries = [
            {
                "operation": "query",
                "tree": "projects",
                "filter": ".workspace.projects[].name",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "projects",
                "filter": ".workspace.projects[] | select(.priority == \"high\") | .name",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "projects",
                "filter": ".workspace.projects[].tasks[] | select(.status == \"in_progress\") | {task: .title, assignee: .assignee}",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(queries)
        batch_data = client.extract_result(result)
        
        print("Query Results:")
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\nQuery {i+1}:")
            if "data" in query_result:
                import json
                print(json.dumps(query_result["data"], indent=2))
        
        # Step 4: Aggregation queries
        print_header("Step 4: Aggregation Queries")
        
        agg_queries = [
            {
                "operation": "query",
                "tree": "projects",
                "filter": "[.workspace.projects[].tasks[].hours] | add",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "projects",
                "filter": ".workspace.projects[].tasks[] | select(.status == \"completed\") | length",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "projects",
                "filter": ".workspace.projects[].team[] | unique",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(agg_queries)
        batch_data = client.extract_result(result)
        
        print("Aggregation Results:")
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\nAggregation {i+1}:")
            if "data" in query_result:
                import json
                print(json.dumps(query_result["data"], indent=2))
        
        # Step 5: Complex nested filtering
        print_header("Step 5: Complex Nested Filtering")
        
        complex_queries = [
            {
                "operation": "query",
                "tree": "projects",
                "filter": """.workspace.projects[] | 
                    {
                        project: .name,
                        active_tasks: [.tasks[] | select(.status != "completed") | .title],
                        completion_rate: (([.tasks[] | select(.status == "completed")] | length) / ([.tasks[]] | length) * 100)
                    }""",
                "verbatim": True
            },
            {
                "operation": "query",
                "tree": "projects",
                "filter": """.workspace.projects[].tasks[] | 
                    select(.subtasks | length > 0) | 
                    {
                        task: .title,
                        subtask_count: (.subtasks | length),
                        all_subtasks_done: ([.subtasks[] | select(.status != "completed")] | length == 0)
                    }""",
                "verbatim": True
            }
        ]
        
        result = client.call_batch(complex_queries)
        batch_data = client.extract_result(result)
        
        print("Complex Query Results:")
        for i, query_result in enumerate(batch_data.get("batch_results", [])):
            print(f"\nComplex Query {i+1}:")
            if "data" in query_result:
                import json
                print(json.dumps(query_result["data"], indent=2))
        
        # Step 6: Transform data structure
        print_header("Step 6: Transform Data Structure")
        
        # Create a by-assignee view
        result = client.call_tool(
            operation="transform",
            tree="projects",
            path=".by_assignee",
            source_path=".workspace.projects",
            filter="""
                [.[].tasks[] | {assignee, task: .title, status, project: "unknown"}] | 
                group_by(.assignee) | 
                map({
                    (.[0].assignee): [.[] | {task, status}]
                }) | 
                add
            """
        )
        print_result("Created assignee view", client.extract_result(result))
        
        # Query the transformed view
        result = client.call_tool(
            operation="query",
            tree="projects",
            filter=".by_assignee",
            verbatim=True
        )
        data = client.extract_result(result)
        print_result("Assignee-centric view", data.get("result"))
        
        # Clean up
        print_header("Complete!")
        if not args.no_clean:
            result = client.call_tool(operation="delete_tree", tree="projects")
            print_result("Cleaned up", client.extract_result(result))
        else:
            print(f"✓ Tree 'projects' preserved in {output_dir}/trees/projects.json")
        
        print("\n" + "="*60)
        print("Key Concepts Demonstrated:")
        print("="*60)
        print("✓ Hierarchical data structures (workspace → projects → tasks → subtasks)")
        print("✓ Efficient batch construction of complex hierarchies")
        print("✓ Navigation with jq path expressions")
        print("✓ Filtering with select() and conditional logic")
        print("✓ Aggregation with add, length, and unique")
        print("✓ Complex nested queries with object construction")
        print("✓ Data transformation for alternative views")
        print("✓ Group-by operations for data reorganization")


if __name__ == "__main__":
    main()
