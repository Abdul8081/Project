import os
from pathlib import Path

# Define the source directory
source_dir = Path.home() / "Desktop" / "Project" / "mgpusim" / "samples"

# Traverse all subdirectories of the source directory
for folder in source_dir.iterdir():
    if folder.is_dir():
        for root, _, files in os.walk(folder):
            for file in files:
                if file.endswith('.csv'):
                    csv_path = Path(root) / file
                    csv_path.unlink()
                    print(f"Deleted: {csv_path}")

print("CSV deletion complete.")
