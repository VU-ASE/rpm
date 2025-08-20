# Processing

This service performs the following steps:

1. For each wheel, it reads the ticks between two black stripes from the embedded RPM sensor over I2C. You can find the I2C definition and the meaning of these ticks [here](http://ase.vu.nl/docs/framework/Software/embedded/rpm/development#i2c-properties)
2. It converts the received data into "normal" time units (milliseconds) and extrapolates the duration of an entire rotation based on the time between two black stripes
  - Note that there are 78 black stripes mounted on the wheel, so one timer value represents 1/78th of a rotation
3. Based on the wheel diameter, the RPM is converted to a speed in meters per second
4. These RPM and speed values, as well as the raw ticks and sequence numbers, are encoded in the [`RpmSensorOutput` message](https://github.com/VU-ASE/rovercom/blob/c1d6569558e26d323fecc17d01117dbd089609cc/definitions/outputs/rpm.proto#L11) which is written to the [`rpm` stream](https://github.com/VU-ASE/rpm/blob/ebdf270c7a98a3b9e7e0e05d3944428a90ffaa29/service.yaml#L13)

**It is highly recommended to check out the embedded RPM docs [here](https://ase.vu.nl/docs/category/rpm-sensor) if you want to better understand the reported values and their meaning.**
