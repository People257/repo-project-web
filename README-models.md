# Model Unification in repo-prompt-web

## Overview

This document describes the unification of data models in the repo-prompt-web project. Previously, the project had duplicate model definitions in multiple locations, which created inconsistencies and maintenance challenges. We've consolidated these models into a single location to improve maintainability and consistency.

## Key Changes

1. **Consolidated Models**: All common models are now defined in `pkg/types/models.go`:
   - `TreeNode`: Represents a node in a file tree
   - `FileContent`: Represents a file's content and metadata
   - `ProcessResult`: Represents the result of processing files
   - `Document`: Represents a documentation file
   - `ProjectAnalysis`: Represents the analysis of a project

2. **Type Aliases**: In domain-specific contexts, we've created type aliases that reference the unified models:
   - `internal/domain/models/file.go`: Contains aliases to the unified models
   - `internal/domain/models/prompt.go`: Contains aliases and a conversion function

3. **Response Consistency**: API responses now use consistent JSON field names and structures:
   - `file_tree` instead of `fileTree`
   - `file_contents` instead of `fileContents`
   - `project_analysis` field for analysis results

## Model Relationships

```
+-----------------+          +------------------+
| TreeNode        |          | FileContent      |
+-----------------+          +------------------+
| name: string    |          | path: string     |
| is_dir: bool    |          | content: string  |
| children: map   |          | is_base64: bool  |
+-----------------+          +------------------+
        |                            |
        |                            |
        v                            v
+---------------------------------------+
| ProcessResult                         |
+---------------------------------------+
| file_tree: TreeNode                   |
| file_contents: map[string]FileContent |
+---------------------------------------+
```

```
+------------------+          +-----------------------+
| Document         |          | ProjectAnalysis       |
+------------------+          +-----------------------+
| path: string     |          | prompt_suggestions    |
| content: string  |--------->| documents             |
| type: string     |          | generated_at          |
+------------------+          +-----------------------+
```

## Benefits

1. **Reduced Duplication**: Eliminated duplicate model definitions
2. **Consistent API Responses**: JSON responses use consistent field naming
3. **Clearer Domain Boundaries**: Domain-specific models extend or reference common models
4. **Improved Maintainability**: Changes to models only need to be made in one place

## Usage Examples

### Creating a TreeNode

```go
root := types.NewTreeNode("", false)
```

### Converting between ContextPrompt and ProjectAnalysis

```go
// Convert from domain-specific model to unified model
analysis := models.ConvertToProjectAnalysis(contextPrompt)
```

### API Response with ProjectAnalysis

```go
c.JSON(http.StatusOK, gin.H{
    "success": true,
    "project_analysis": projectAnalysis,
})
``` 