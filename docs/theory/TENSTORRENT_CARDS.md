# Tenstorrent PCIe Card Reference

This document summarizes the current public card-level specs and how they map to
practical deployment use cases.

## References

- Tenstorrent hardware card index: [Cards][]
- Blackhole PCIe cards docs: [Blackhole documentation][]
- Wormhole PCIe cards docs: [Wormhole documentation][]

## Blackhole Cards (Current-gen AI)

Tenstorrent describes Blackhole as a high-performance AI card family with 120
Tensix cores and 16 SiFive x280 “Big RISC-V” cores on each card processor.

### Blackhole card specs

| Card | PCIe | AI Clock | Tensix Cores | SRAM | HBM / GDDR | Memory BW | BLOCKFP8 TFLOPS | TBP | Power | Cooling | Interconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| p100a | PCIe 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 28 GB GDDR6 (256-bit) | 448 GB/s | 664 | 300 W | Active | No QSFP ports |  |
| p150a | PCIe 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 32 GB GDDR6 (256-bit) | 512 GB/s | 664 | 300 W | Active | 4 × QSFP-DD 800G (passive) | up to 2 m cable |
| p150b | PCIe 5.0 ×16 | Up to 1.35 GHz | 120 | 180 MB | 32 GB GDDR6 (256-bit) | 512 GB/s | 664 | 300 W | Passive | 4 × QSFP-DD 800G (passive) | up to 2 m cable |

Notes:

- p100a/p150a are aimed at desktop/workstation use where active cooling is expected.
- p150b is intended for rack or other environments with sufficient forced airflow.
- All three use a 12+4-pin 12V-2x6 power connector and target x86_64 host systems.
- Data precision supported on Blackhole Tensix includes FP8/FP16/BF16 plus block formats
  (BLOCKFP2/4/8); Big RISC-V cores add additional precision coverage in float formats.

## Wormhole Cards (Cost-effective, scalable)

Wormhole is positioned as scalable, flexible AI hardware with support for
multichip mesh networking and both high-level and low-level software stacks.

### Wormhole card specs

| Card | PCIe | AI Clock | ASICs | Tensix Cores | SRAM | HBM / GDDR | Memory BW | FP8 TFLOPS | FP16 TFLOPS | BLOCKFP8 TFLOPS | TBP | Power | Cooling | Interconnect |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| n150d | PCIe 4.0 ×16 | 1 GHz | 1 | 72 | 108 MB | 12 GB GDDR6 (192-bit) | 288 GB/s | 262 | 74 | 148 | 160 W | Active (axial fan) | 2 × QSFP-DD 200G (active), 2 × Warp 100 |
| n150s | PCIe 4.0 ×16 | 1 GHz | 1 | 72 | 108 MB | 12 GB GDDR6 (192-bit) | 288 GB/s | 262 | 74 | 148 | 160 W | Passive | 2 × QSFP-DD 200G (active), 2 × Warp 100 |
| n300d | PCIe 4.0 ×16 | 1 GHz | 2 | 128 (64 per ASIC) | 192 MB (96 per ASIC) | 24 GB GDDR6 | 576 GB/s | 466 | 131 | 262 | 300 W | Active (axial fan) | 2 × QSFP-DD 200G (active), 2 × Warp 100 |
| n300s | PCIe 4.0 ×16 | 1 GHz | 2 | 128 (64 per ASIC) | 192 MB (96 per ASIC) | 24 GB GDDR6 | 576 GB/s | 466 | 131 | 262 | 300 W | Passive | 2 × QSFP-DD 200G (active), 2 × Warp 100 |

Notes:

- n150/n300 cards are optimized around a 4+4-pin EPS12V host connector and
  x86_64 hosts with at least 64 GB RAM.
- n150d/n150s are single-chip cards; n300d/n300s are dual-chip.
- n150s and n300s ship with passive cooling; if your host lacks enough forced
  airflow, use an Active Cooling Kit path for safe operation.
- Data precision support includes FP8/FP16/BF16 and integer/binary formats such as
  INT8 and TF32 support for Tensix workloads.

## Recommended use cases by real-world scenario

### When to use Blackhole

- You need the highest per-card AI throughput in this lineup, especially with
  large memory and high memory-bandwidth requirements.
- You need lower-latency model serving or bigger per-card context windows that
  benefit from 28–32 GB local memory.
- You want to run tightly coupled multi-card fabrics using 4 × 800G links on
  p150a/p150b for Blackhole-only clusters.

### When to use Wormhole

- You need scalable AI acceleration with strong performance per watt profile and a
  lower entry point per node.
- Your cluster benefits from multichip meshing and/or mesh-style topologies (for
  “Galaxy”-style deployments).
- You need a balance between single-card efficiency (n150 variants) and higher
  aggregate compute (n300 variants).
- You want compatibility with both TT-Buda (higher-level) and TT-Metalium (lower-level)
  software approaches.

## Quick selection heuristics

- **Start with p100a / p150a** for early workstation deployments and smaller-scale
  validation.
- **Choose p150b** for rack-facing Blackhole capacity where cooling and physical
  acoustics are prioritized.
- **Choose n150d/n150s** for cost-sensitive nodes needing good AI throughput.
- **Choose n300d/n300s** for higher aggregate compute and memory where node count is
  constrained.

[blackhole documentation]: https://docs.tenstorrent.com/aibs/blackhole/index.html
[wormhole documentation]: https://docs.tenstorrent.com/aibs/wormhole/index.html
[cards]: https://tenstorrent.com/hardware/cards
