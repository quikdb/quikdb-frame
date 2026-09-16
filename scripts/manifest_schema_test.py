"""Remote validation of the public schema against the shared contract corpus."""
import json
from pathlib import Path
from jsonschema import Draft202012Validator
root = Path(__file__).resolve().parents[1]
schema = json.loads((root / "contracts/deployment-manifest-v1.schema.json").read_text())
Draft202012Validator.check_schema(schema)
validator = Draft202012Validator(schema)
cases = json.loads((root / "contracts/deployment-manifest-v1.cases.json").read_text())
for case in cases:
    expected = case.get("schemaValid", case["valid"])
    assert validator.is_valid(case["manifest"]) == expected, case["name"]
print(f"Public schema: {len(cases)} contract cases passed; cross-field Node consistency is an additional runtime rule.")

project_schema = json.loads((root / "contracts/frame-project-manifest-v1.schema.json").read_text())
Draft202012Validator.check_schema(project_schema)
project_validator = Draft202012Validator(project_schema)
project_cases = json.loads((root / "contracts/frame-project-manifest-v1.cases.json").read_text())
for case in project_cases:
    assert project_validator.is_valid(case["manifest"]) == case["valid"], case["name"]
print(f"Frame project schema: {len(project_cases)} contract cases passed; service references, route parity and dependency cycles are additional runtime rules.")
