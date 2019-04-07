# Bitcoin-Feeestimator

Research on predicting Bitcoin transaction fees and coin selection.

## Context

Blockchain-technologies such as Bitcoin and other crypto currencies are currently entering a more mature stage. However, the user experience is still lacking in several areas. One crucial point regarding payments is the optimization of transaction fees. In many cases the basic functionality is covered by the concrete wallet implementations which often make use of suboptimal naïve approaches. In this paper approaches to estimate fees and select coins are challenged and improvements are discussed.

The Bitcoin-blockchain is using a construct called “unspent transaction output” (UTXO). UTXOs are used as inputs for a payment (transaction) and the associated fees. UTXOs are often referred to as coins. When a user wants to send Bitcoin, she has to choose exactly which “inputs” (UTXOs) to use. Furthermore, she has to determine how much she is willing to pay for the transaction (transaction fee) to be processed contemporary (say why). The selection of coins is in most cases implemented by the wallet. If this selection is done right, the transaction fees can be reduced significantly. However, the problem of choosing the right parameters is considered NP-hard which means that if the user has many coins it is not efficiently solvable. Additionally, the goals for choosing the right coins are conflicting to a certain extent. The different goals include:

- **low transaction fees**
- **avoidance of dust**: If a UTXO is too small, it is considered dust which means it is not worth spending anymore.
- **reduction of the UTXO pool**: every UTXO has to be stored on the blockchain. If the UTXO pool grows additional data has to be stored on the blockchain, which automatically leads to higher storage costs for the network participants, particularly for the full nodes, which store all the information. Pérez-Sola et al. [1] show an analysis of the current UTXO set and the proportion of dust.
- **privacy**: every transaction includes a certain amount of change which is going back to the sender. If the sender is using a hierarchical deterministic wallet the change address is different to the sending address for the sake of privacy. However, if the algorithm for selecting coins is always producing the same outcome it is inherent which output of a transaction is the change and therefore the change address can be associated with the sender which leads to a lack of privacy.

For example, the avoidance of dust might conflict with low transaction fees.

In this repository methods for estimating transaction fees in Bitcoin are analyzed. In a second step a simulation done in previous work is extended with an algorithm for selecting coins with the goal to minimize fees. The algorithm provided is compared to existing algorithms.

## Build

```bash
dep ensure
go build -o ./output/estimator . && ./output/estimator
```

## Generate pseudo code

```bash
brew install pandoc
pandoc Pseudo.mdc -o Pseudo.pdf
```
