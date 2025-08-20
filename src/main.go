package main

import (
	"fmt"
	"math"
	"os"
	"time"

	movingaverage "github.com/RobinUS2/golang-moving-average"
	pb_outputs "github.com/VU-ASE/rovercom/v2/packages/go/outputs"
	roverlib "github.com/VU-ASE/roverlib-go/v2/src"

	"github.com/d2r2/go-i2c"
	"github.com/rs/zerolog/log"
)

// I2C configuration
const (
	I2C_BUS         = 5
	I2C_DEVICE_ADDR = 0x14
	// Timer magic
	TIMER_CLOCK_FREQ = 8000000.0 // 8 MHz clock
	// RPM calculation
	STRIPE_COUNT = 78
)

// Register overview for the I2C device
//  Register 0       Register 1        Register 2       Register 3
// ┌────────────────┬────────────────┬────────────────┬────────────────┐
// │ 0xba           │ 0xbe           │ Timer Capt. L  │ Timer Capt. L  │
// │ Control value  │ Control value  │ 1/4            │ 2/4            │
// └────────────────┴────────────────┴────────────────┴────────────────┘

//  Register 4       Register 5        Register 6       Register 7
// ┌────────────────┬────────────────┬────────────────┬────────────────┐
// │ Timer Capt. L  │ Timer Capt. L  │ Timer Capt. R  │ Timer Capt. R  │
// │ 3/4            │ 4/4            │ 1/4            │ 2/4            │
// └────────────────┴────────────────┴────────────────┴────────────────┘

//  Register 8       Register 9        Register 10      Register 11
// ┌────────────────┬────────────────┬────────────────┬────────────────┐
// │ Timer Capt. R  │ Timer Capt. R  │ Timeout Cnt. L │ Timeout Cnt. L │
// │ 3/4            │ 4/4            │ 1/2            │ 2/2            │
// └────────────────┴────────────────┴────────────────┴────────────────┘

//  Register 12      Register 13       Register 14      Register 15
// ┌────────────────┬────────────────┬────────────────┬────────────────┐
// │ Timeout Cnt. R │ Timeout Cnt. R │ Sequence No. L │ Sequence No. L │
// │ 1/2            │ 2/2            │ 1/2            │ 1/2            │
// └────────────────┴────────────────┴────────────────┴────────────────┘

//  Register 16      Register 17       Register 18 .... Register 255
// ┌────────────────┬────────────────┬─────────────────────────────────┐
// │ Sequence No. R │ Sequence No. R │              0x0                │
// │ 1/2            │ 2/2            │             Unused              │
// └────────────────┴────────────────┴─────────────────────────────────┘

type I2Cresult struct {
	Left  *pb_outputs.MotorInformation
	Right *pb_outputs.MotorInformation
}

func readI2CRegisters(device *i2c.I2C) (*I2Cresult, error) {
	// Read 18 bytes starting from register 0x00
	data, _, err := device.ReadRegBytes(0x00, 18)
	if err != nil {
		return nil, fmt.Errorf("failed to read I2C registers: %v", err)
	}

	if len(data) < 18 {
		return nil, fmt.Errorf("received less than 18 bytes from I2C device")
	}

	// Try to parse the data from the registers using the above overview
	// in little endian format

	timerCaptureLeft := (uint32(data[2]) << 24) |
		(uint32(data[3]) << 16) |
		(uint32(data[4]) << 8) |
		uint32(data[5])

	timerCaptureRight := (uint32(data[6]) << 24) |
		(uint32(data[7]) << 16) |
		(uint32(data[8]) << 8) |
		uint32(data[9])

	timeoutCountLeft := (uint16(data[10]) << 8) | uint16(data[11])
	timeoutCountRight := (uint16(data[12]) << 8) | uint16(data[13])

	sequenceNoLeft := (uint16(data[14]) << 8) | uint16(data[15])
	sequenceNoRight := (uint16(data[16]) << 8) | uint16(data[17])

	// Left
	timerCaptureInMsLeft := ((1 / TIMER_CLOCK_FREQ) * 1000.0 * float64(timerCaptureLeft))
	leftRpm := (60.0 * 1000.0) / (timerCaptureInMsLeft * STRIPE_COUNT)
	speedLeft := (leftRpm / 60.0) * (math.Pi * 0.064) // in ms
	// Right
	timerCaptureInMsRight := ((1 / TIMER_CLOCK_FREQ) * 1000.0 * float64(timerCaptureRight))
	rightRpm := (60.0 * 1000.0) / (timerCaptureInMsRight * STRIPE_COUNT)
	speedRight := (rightRpm / 60.0) * (math.Pi * 0.064) // in ms

	leftInfo := pb_outputs.MotorInformation{
		Ticks:          timerCaptureLeft,
		TimeoutCount:   uint32(timeoutCountLeft),
		SequenceNumber: uint32(sequenceNoLeft),
		Speed:          float32(speedLeft),
		Rpm:            float32(leftRpm),
	}

	rightInfo := pb_outputs.MotorInformation{
		Ticks:          timerCaptureRight,
		TimeoutCount:   uint32(timeoutCountRight),
		SequenceNumber: uint32(sequenceNoRight),
		Speed:          float32(speedRight),
		Rpm:            float32(rightRpm),
	}

	return &I2Cresult{
		Left:  &leftInfo,
		Right: &rightInfo,
	}, nil
}

// The main user space program
func run(service roverlib.Service, configuration *roverlib.ServiceConfiguration) error {
	// Open I2C device
	device, err := i2c.NewI2C(I2C_DEVICE_ADDR, I2C_BUS)
	if err != nil {
		return fmt.Errorf("failed to open I2C device: %v", err)
	}
	defer device.Close()

	// Writing to an output that other services can read
	writeStream := service.GetWriteStream("rpm")
	if writeStream == nil {
		return fmt.Errorf("Failed to create write stream 'rpm'")
	}

	log.Info().Msg("Starting RPM service")

	maLeft := movingaverage.New(6)
	maRight := movingaverage.New(6)

	for {
		// Read I2C values from your PIC
		motorInfo, err := readI2CRegisters(device)
		if err != nil {
			log.Warn().Err(err).Msg("Could not read I2C registers")
			continue
		}

		log.Debug().
			Uint32("left_raw", motorInfo.Left.Ticks).
			Uint32("right_raw", motorInfo.Right.Ticks).
			Msg("I2C raw register readings")

		maLeft.Add(float64(motorInfo.Left.Speed))
		maRight.Add(float64(motorInfo.Right.Speed))

		motorInfo.Left.Speed = float32(maLeft.Avg())
		motorInfo.Right.Speed = float32(maRight.Avg())

		// Convert the clock ticks to ms
		// overflow at 5 ms, so use the overflow counter

		// Initialize the message with raw values for debugging
		actuatorMsg := pb_outputs.SensorOutput{
			Timestamp: uint64(time.Now().UnixMilli()),
			Status:    0,
			SensorId:  1,
			SensorOutput: &pb_outputs.SensorOutput_RpmOutput{
				RpmOutput: &pb_outputs.RpmSensorOutput{
					LeftMotor:  motorInfo.Left,
					RightMotor: motorInfo.Right,
				},
			},
		}

		// Send the message
		err = writeStream.Write(&actuatorMsg)
		if err != nil {
			log.Warn().Err(err).Msg("Could not write to actuator")
		}

		// Wait before next reading
		time.Sleep(100 * time.Millisecond) // 10Hz update rate
	}
}

// This function gets called when roverd wants to terminate the service
func onTerminate(sig os.Signal) error {
	log.Info().Str("signal", sig.String()).Msg("Terminating service")
	return nil
}

func main() {
	roverlib.Run(run, onTerminate)
}
