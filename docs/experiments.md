# List of experiments that can be enabled

## Example config
```
 "Experiments": {
    "<<ExperimentName>>": {
      "Enabled": true,
      "Value": 0.1,
    }
}
```
 * Where `<<ExperimentName>>` is the name of the experiment

## Experiment: NodeUnknownAsReady
 * If enabled, megamon will treat a fraction (defined by the experiment `Value`) 
of the nodes as ready when LastTransitionTime is less than 3 minutes
 * `Value` - valid values from `0` to `1.0`
 * e.g.
   * if `Value` is 0.1 for a nodepool with 16 nodes expected, megamon will tolerate: `math.RoundEven(16*0.1)==2` nodes unknown as ready
     * If we have 14 nodes ready, 2 unknown, megamon reports the nodepool as up
     * If we have 14 nodes ready, 1 unknown, megamon reports the nodepool as down (there is 1 node implied as not ready)