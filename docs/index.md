---
layout: home

hero:
  name: Aether
  text: FHIR Data Processing Tool
  tagline: Extract, pseudonymize, flatten, and send FHIR data
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started/installation
    - theme: alt
      text: Configuration
      link: /getting-started/configuration

features:
  - icon: 🔗
    title: TORCH Integration
    details: Extract patient data from TORCH servers using CRTDL queries
    link: /guides/torch-integration
  - icon: 🔐
    title: DIMP Pseudonymization
    details: De-identify and pseudonymize FHIR data to protect patient privacy
    link: /guides/dimp
  - icon: 📊
    title: FHIR to CSV
    details: Transform FHIR NDJSON into CSV using SQL-on-FHIR ViewDefinitions
    link: /guides/steps/flattening
  - icon: ⏸️
    title: Wait
    details: Pause pipeline for manual data inspection or modification
    link: /guides/steps/wait
  - icon: 📤
    title: Send
    details: Upload data to FHIR servers or package for DSF transfer
    link: /guides/steps/send
  - icon: ⚙️
    title: Simple Configuration
    details: One YAML file to configure everything with environment variable support
    link: /getting-started/configuration
---
