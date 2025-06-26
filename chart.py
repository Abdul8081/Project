# import os
# import csv
# import matplotlib.pyplot as plt

# # Base directory (adjust if needed)
# base_dir = os.path.expanduser("~/Desktop/Project/mgpusim/samples")

# # Store results
# folders = []
# hit_rates = []
# miss_rates = []

# # Traverse folders
# for folder_name in os.listdir(base_dir):
#     folder_path = os.path.join(base_dir, folder_name)
#     if not os.path.isdir(folder_path):
#         continue

#     # Find a CSV file in the folder
#     csv_file = None
#     for file_name in os.listdir(folder_path):
#         if file_name.endswith('.csv'):
#             csv_file = os.path.join(folder_path, file_name)
#             break

#     if not csv_file:
#         continue  # Skip if no CSV file

#     # Counters
#     total_hit = 0
#     total_miss = 0

#     # Read the CSV file
#     with open(csv_file, mode='r') as file:
#         reader = csv.DictReader(file, skipinitialspace=True)
#         for row in reader:
#             what = row['what'].strip()
#             value = float(row['value'])

#             if what == 'hit':
#                 total_hit += value
#             elif what == 'miss':
#                 total_miss += value
#             # skip mshr-hit

#     total_accesses = total_hit + total_miss
#     if total_accesses == 0:
#         continue  # Avoid divide-by-zero

#     hit_rate = total_hit / total_accesses
#     miss_rate = total_miss / total_accesses

#     folders.append(folder_name)
#     hit_rates.append(hit_rate)
#     miss_rates.append(miss_rate)

# # Plotting
# x = range(len(folders))
# bar_width = 0.35

# plt.figure(figsize=(12, 6))
# plt.bar(x, hit_rates, width=bar_width, label='Hit Rate', color='green')
# plt.bar([i + bar_width for i in x], miss_rates, width=bar_width, label='Miss Rate', color='red')

# plt.xlabel("Folder")
# plt.ylabel("Rate")
# plt.title("Hit and Miss Rates per Folder (L1VTLB)")
# plt.xticks([i + bar_width / 2 for i in x], folders, rotation=45, ha='right')
# plt.legend()
# plt.tight_layout()

# # Show the graph
# plt.show()



import os
import csv
import matplotlib.pyplot as plt
from collections import defaultdict

# Base directory
base_dir = os.path.expanduser("~/Desktop/Project/mgpusim/samples")

# Data structure: {csv_filename: {folder: (hit_rate, miss_rate)}}
csv_data = defaultdict(dict)

# Traverse folders
for folder_name in os.listdir(base_dir):
    folder_path = os.path.join(base_dir, folder_name)
    if not os.path.isdir(folder_path):
        continue

    # Look for all CSV files
    for file_name in os.listdir(folder_path):
        if not file_name.endswith('.csv'):
            continue

        csv_path = os.path.join(folder_path, file_name)

        total_hit = 0
        total_miss = 0

        # Read the CSV file
        with open(csv_path, mode='r') as file:
            reader = csv.DictReader(file, skipinitialspace=True)
            for row in reader:
                what = row['what'].strip()
                value = float(row['value'])

                if what == 'hit':
                    total_hit += value
                elif what == 'miss':
                    total_miss += value
                # skip mshr-hit

        total = total_hit + total_miss
        if total == 0:
            continue  # skip if no accesses

        hit_rate = total_hit / total
        miss_rate = total_miss / total

        csv_data[file_name][folder_name] = (hit_rate, miss_rate)

# Plot one chart per CSV filename
for csv_file, folder_results in csv_data.items():
    folders = list(folder_results.keys())
    hit_rates = [folder_results[f][0] for f in folders]
    miss_rates = [folder_results[f][1] for f in folders]

    x = range(len(folders))
    bar_width = 0.35

    plt.figure(figsize=(12, 6))
    plt.bar(x, hit_rates, width=bar_width, label='Hit Rate', color='green')
    plt.bar([i + bar_width for i in x], miss_rates, width=bar_width, label='Miss Rate', color='red')

    plt.xlabel("Folder")
    plt.ylabel("Rate")
    plt.title(f"Hit/Miss Rate per Folder for '{csv_file}'")
    plt.xticks([i + bar_width / 2 for i in x], folders, rotation=45, ha='right')
    plt.legend()
    plt.tight_layout()
    plt.show()
