import os
import shutil
from pathlib import Path

# Define source and target directories
source_dir = Path.home() / "Desktop" / "Project" / "mgpusim" / "samples"
output_dir = Path.home() / "Desktop" / "CollectedCSVs"

# Create the output directory if it doesn't exist
output_dir.mkdir(parents=True, exist_ok=True)

# Traverse all subdirectories of the source directory
for folder in source_dir.iterdir():
    if folder.is_dir():
        folder_name = folder.name
        for root, _, files in os.walk(folder):
            for file in files:
                if file.endswith('.csv'):
                    csv_path = Path(root) / file
                    # Rename the file to include the folder name
                    new_file_name = f"{folder_name}_{file}"
                    target_path = output_dir / new_file_name
                    shutil.copy(csv_path, target_path)
                    print(f"Copied: {csv_path} -> {target_path}")

print("CSV collection complete.")
