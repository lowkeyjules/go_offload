import os
import pandas as pd
import matplotlib.pyplot as plt

METHOD_ORDER = ["Local", "Submit", "SubmitAll", "Dispatch", "DispatchAll"]


def plot_per_task(df, outdir="plots"):
    os.makedirs(outdir, exist_ok=True)

    methods = [m for m in METHOD_ORDER if m in df["method"].unique()]
    batch_sizes = sorted(df["batch_size"].unique())

    df = df.copy()
    df["per_task_ms"] = df["duration_ms"] / df["batch_size"]

    stats = (
        df.groupby(["method", "batch_size"])["per_task_ms"]
        .agg(["mean", "std"])
        .reset_index()
    )

    plt.figure()
    for m in methods:
        sub = stats[stats["method"] == m].sort_values("batch_size")
        plt.errorbar(sub["batch_size"], sub["mean"], yerr=sub["std"],
                     marker="o", capsize=3, label=m)
    plt.xlabel("batch size (tasks per call)")
    plt.ylabel("per-task duration [ms]")
    plt.title("Per-task cost vs batch size")
    plt.xticks(batch_sizes)
    plt.legend()
    plt.grid(True, alpha=0.3)
    plt.savefig(os.path.join(outdir, "4_per_task_cost.png"),
                dpi=150, bbox_inches="tight")
    print(f"saved per-task plot to {outdir}/")


if __name__ == "__main__":
    print(os.getcwd())
    df = pd.read_csv(os.path.join("results", "api_bench.csv"))
    plot_per_task(df)