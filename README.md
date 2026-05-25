# NEXUS

## Research Nexus

As part of my work at Crossref as SRE, we've been working towards building the "[Research Nexus](https://www.crossref.org/documentation/research-nexus/)" which basically means taking all of the metadata about journals, articles, books, people and forming relationships/links between them.

This repo is meant to act as a prototype for a system design to achieve this goal.

Crossref creates a snapshot of all their data (~250GB compressed) which is published once per year, after which you can use their API to get more recent updates. Other companies like ROR and Orcid also publish their data in a similar way. The Research Nexus would require us to get all of that data, link it together and publish the updated data.

The linking is done in different ways depending on the data type. For example, you could find that Crossref metadata contains a string with the name of an organization which appears in the ROR data. You could add the ROR ID for that organization to the Crossref metadata to make a hard link between the two data.

## Prototype

This project will serve as a prototype for the Nexus. It will consist of multiple systems which run independently and work together to gather, process and output data.

The weird system names come from [factorio: space age](https://www.youtube.com/watch?v=OiczN-8QKDA) because the themes of each planet are quite similar to the system design.

```
Nauvis    Fulgora
  |          |
  >   CROID  <
        |
        ^
Vulcanus Gleba
        v
        |
        |
        ^
  Aquilo Nexus
        v
        |
      Pomus 
```

In the `systems` folder each system has its own folder for storing its documentation and code.

### Nauvis

- Downloads the Crossref snapshot and stores it in a format readable by other systems

### Fulgora

- Downloads data and snapshots from other organizations and stores it in a format readable by other systems

### CROID

- Generates identifier strings to assign to data so that it can be tracked across systems

### Vulcanus

- Data pipelines to take the collected data and process it

### Gleba

- Keeps track of statistics associated with data, like number of citations associated with a journal article

### Aquilo

- The main API for accessing the processed data

### Nexus

- Generate relationships and provenance data based on the metadata

### Pomus

- A human user interface for accessing the processed data

### Storage

- Define the different types of and ways to access data storage to be used across systems.

## Documentation

The main documentation is stored in the `documentation` folder at the top level of the git repo. Then each system in the `systems` folder has the system documentation in the `docs` folders. The documentation site is generated using the `mkdocs` tool and will gather all of the documentation from each system folder.
