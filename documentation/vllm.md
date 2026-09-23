# VLLM

These all use the NVFP4 quant so that they should run faster on 2 x 5060 Ti 16GB GPUs which have Blackwell architecture.

## Nemotron 3.5 Lightning 30b-a3b

```
podman run --device nvidia.com/gpu=all \
-v ~/models/nemotron:/root/nemotron \
-p 8000:8000 \
--ipc=host \
docker.io/vllm/vllm-openai:latest \
    /root/nemotron/ \
    --served-model-name "nemotron3.5:30b-a3b" \
    --tensor-parallel-size 2 \
    --quantization nvfp4 \
    --kv-cache-dtype fp8 \
    --gpu-memory-utilization 0.90 \
    --max-model-len 131072 \
    --port 8000 \
    --reasoning-parser nemotron_v3 \
    --tool-call-parser qwen3_coder \
    --moe-backend marlin \
    --enable-auto-tool-choice
```

## Gemma4 26b-a4b

```
podman run --device nvidia.com/gpu=all \
-v ~/models/gemma426b:/root/gemma426b \
-p 8000:8000 \
--ipc=host \
docker.io/vllm/vllm-openai:latest \
    /root/gemma426b \
    --tensor-parallel-size 2 \
    --kv-cache-dtype fp8 \
    --moe-backend marlin \
    --max-model-len 131072 \
    --gpu-memory-utilization 0.90 \
    --port 8000 \
    --reasoning-parser gemma4 \
    --tool-call-parser gemma4 \
    --default-chat-template-kwargs '{"enable_thinking": true}' \
    --enable-auto-tool-choice \
    --served-model-name "gemma4:26b-a4b" \
    --language-model-only
```

## Qwen3.6 35b-a3b

```
podman run --device nvidia.com/gpu=all \
-v ~/models/qwen36:/root/qwen36 \
-p 8000:8000 \
--ipc=host \
docker.io/vllm/vllm-openai:latest \
    /root/qwen36 \
    --tensor-parallel-size 2 \
    --kv-cache-dtype fp8 \
    --max-model-len 131072 \
    --gpu-memory-utilization 0.90 \
    --port 8000 \
    --reasoning-parser qwen3 \
    --tool-call-parser qwen3_coder \
    --enable-auto-tool-choice \
    --served-model-name "qwen3.6:35b-a3b" \
    --moe-backend marlin \
    --max-num-seqs 8 \
    --max-num-batched-tokens 8192 \
    --enable-chunked-prefill \
    --enable-prefix-caching \
    --language-model-only
```

## Qwen 3.8 27b

```
podman run --device nvidia.com/gpu=all \
-v ~/models/qwen38:/root/qwen38 \
-p 8000:8000 \
--ipc=host \
docker.io/vllm/vllm-openai:latest \
    /root/qwen38 \
    --tensor-parallel-size 2 \
    --kv-cache-dtype fp8_e4m3 \
    --max-model-len 131072 \
    --gpu-memory-utilization 0.90 \
    --enable-chunked-prefill \
    --max-num-batched-tokens 8192 \
    --enable-prefix-caching \
    --max-num-seqs 4 \
    --port 8000 \
    --reasoning-parser qwen3 \
    --tool-call-parser qwen3_coder \
    --enable-auto-tool-choice \
    --served-model-name "qwen3.8:27b" \
    --language-model-only
```
