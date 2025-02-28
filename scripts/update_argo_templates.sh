#!/bin/bash

# NOTE: run inside the templates dir
for i in $(pwd)/*.tmpl; do
  sed -i '
    # Change the project field to kubeaid
    s/^ *project:.*$/  project: kubeaid/

    # Add labels under metadata
    /^metadata:/a\
    labels:\
      kubeaid.io/version: "9.1.0"\
      kubeaid.io/managed-by: "kubeaid"\
      kubeaid.io/priority: "default"\
' $i

  # Remove any trailing empty lines
  sed -i /^$/d $i
done
