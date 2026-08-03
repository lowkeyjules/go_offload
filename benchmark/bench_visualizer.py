import os
import pandas as pd
import matplotlib.pyplot as plt

# fixed order so legends / bars are always in the same sequence
METHOD_ORDER = ["Local", "Submit", "SubmitAll", "Dispatch", "DispatchAll"]


def plt_df(df, outdir="plots"):
    os.makedirs(outdir, exist_ok=True)

    methods = [m for m in METHOD_ORDER if m in df["method"].unique()]
    batch_sizes = sorted(df["batch_size"].unique())
    max_batch = max(batch_sizes)

    # mean + std of duration per (method, batch size)
    stats = (
        df.groupby(["method", "batch_size"])["duration_ms"]
        .agg(["mean", "std"])
        .reset_index()
    )

    # ---- mean duration vs batch size, one line per method ----
    plt.figure()
    for m in methods:
        sub = stats[stats["method"] == m].sort_values("batch_size")
        plt.errorbar(sub["batch_size"], sub["mean"], yerr=sub["std"],
                     marker="o", capsize=3, label=m)
    plt.xlabel("batch size (tasks per call)")
    plt.ylabel("duration [ms]")
    plt.title("Mean duration vs batch size")
    plt.xticks(batch_sizes)
    plt.legend()
    plt.grid(True, alpha=0.3)
    plt.savefig(os.path.join(outdir, "1_latency_vs_batch.png"),
                dpi=150, bbox_inches="tight")

    # largest batchsize - where the differences are biggest
    plt.figure()
    at_max = (stats[stats["batch_size"] == max_batch]
              .set_index("method").reindex(methods))
    plt.bar(at_max.index, at_max["mean"], yerr=at_max["std"], capsize=4)
    plt.ylabel("duration [ms]")
    plt.title(f"Method comparison at batch size {max_batch}")
    plt.grid(True, axis="y", alpha=0.3)
    plt.savefig(os.path.join(outdir, "2_bar_at_max_batch.png"),
                dpi=150, bbox_inches="tight")

    # ---- speedup relative to sequential Submit ----
    # how much batching / async actually buys you
    if "Submit" in methods:
        plt.figure()
        submit_mean = stats[stats["method"] == "Submit"].set_index("batch_size")["mean"]
        for m in methods:
            if m == "Submit":
                continue
            sub = stats[stats["method"] == m].set_index("batch_size")["mean"]
            speedup = (submit_mean / sub).sort_index()
            plt.plot(speedup.index, speedup.values, marker="o", label=m)
        plt.axhline(1.0, color="gray", linestyle="--", linewidth=1)
        plt.xlabel("batch size (tasks per call)")
        plt.ylabel("speedup vs Submit (x)")
        plt.title("Speedup relative to sequential Submit")
        plt.xticks(batch_sizes)
        plt.legend()
        plt.grid(True, alpha=0.3)
        plt.savefig(os.path.join(outdir, "3_speedup_vs_submit.png"),
                    dpi=150, bbox_inches="tight")

    print(f"saved plots to {outdir}/")


if __name__ == "__main__":
    print(os.getcwd())
    for name in ["api_bench.csv"]:
        path = os.path.join("results", name)
        df = pd.read_csv(path)
        plt_df(df)