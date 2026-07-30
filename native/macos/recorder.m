// sessionhub-voice-recorder: a small, standalone command-line microphone
// recorder for macOS, built and shipped alongside SessionHub's self-hosted
// whisper.cpp binaries (see .github/workflows/release.yml's
// macos-voice-tools job). SessionHub's Go binary stays CGO_ENABLED=0 on
// every platform — this native helper exists so that constraint doesn't
// have to change; internal/voice/recorder_darwin.go just shells out to it
// via os/exec, the same way it already shells out to whisper-server.exe on
// Windows.
//
// Usage: sessionhub-voice-recorder <output.wav>
// Records the default audio input device to <output.wav> (16kHz mono
// 16-bit linear PCM) until it receives SIGINT or SIGTERM, then flushes and
// exits 0. Any setup failure prints a reason to stderr and exits non-zero,
// so the Go side can surface a useful error instead of a silent no-op.
//
// macOS requires the user to have granted microphone access to whatever
// terminal app launches this process (System Settings > Privacy &
// Security > Microphone) — there is no way to prompt for that from a
// plain, unbundled command-line tool.

#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>
#include <signal.h>
#include <stdio.h>
#include <unistd.h>

static volatile sig_atomic_t gStopRequested = 0;

static void handleStopSignal(int sig) {
    (void)sig;
    gStopRequested = 1;
}

@interface RecordingDelegate : NSObject <AVCaptureFileOutputRecordingDelegate>
@property(nonatomic) dispatch_semaphore_t doneSemaphore;
@property(nonatomic) NSError *finishError;
@end

@implementation RecordingDelegate

- (void)captureOutput:(AVCaptureFileOutput *)output
    didFinishRecordingToOutputFileAtURL:(NSURL *)outputFileURL
                        fromConnections:(NSArray *)connections
                                  error:(NSError *)error {
    (void)output;
    (void)outputFileURL;
    (void)connections;
    self.finishError = error;
    dispatch_semaphore_signal(self.doneSemaphore);
}

@end

int main(int argc, const char *argv[]) {
    @autoreleasepool {
        if (argc < 2) {
            fprintf(stderr, "usage: %s <output.wav>\n", argv[0]);
            return 1;
        }
        NSString *outputPath = [NSString stringWithUTF8String:argv[1]];
        NSURL *outputURL = [NSURL fileURLWithPath:outputPath];

        signal(SIGINT, handleStopSignal);
        signal(SIGTERM, handleStopSignal);

        AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeAudio];
        if (device == nil) {
            fprintf(stderr, "no default audio input device found\n");
            return 2;
        }

        NSError *error = nil;
        AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
        if (input == nil) {
            fprintf(stderr, "failed to open audio input %s: %s\n",
                    device.localizedName.UTF8String,
                    error.localizedDescription.UTF8String);
            return 2;
        }

        AVCaptureSession *session = [[AVCaptureSession alloc] init];
        if (![session canAddInput:input]) {
            fprintf(stderr, "capture session cannot accept the audio input\n");
            return 2;
        }
        [session addInput:input];

        AVCaptureAudioFileOutput *fileOutput = [[AVCaptureAudioFileOutput alloc] init];
        if (![session canAddOutput:fileOutput]) {
            fprintf(stderr, "capture session cannot accept the audio file output\n");
            return 2;
        }
        [session addOutput:fileOutput];

        // 16kHz mono 16-bit linear PCM: what whisper.cpp expects natively,
        // so no resampling step is needed downstream.
        fileOutput.audioSettings = @{
            AVFormatIDKey : @(kAudioFormatLinearPCM),
            AVSampleRateKey : @(16000.0),
            AVNumberOfChannelsKey : @(1),
            AVLinearPCMBitDepthKey : @(16),
            AVLinearPCMIsFloatKey : @NO,
            AVLinearPCMIsBigEndianKey : @NO,
        };

        RecordingDelegate *delegate = [[RecordingDelegate alloc] init];
        delegate.doneSemaphore = dispatch_semaphore_create(0);

        [session startRunning];
        [fileOutput startRecordingToOutputFileURL:outputURL
                                    outputFileType:AVFileTypeWAVE
                                 recordingDelegate:delegate];

        fprintf(stderr, "recording from %s to %s (send SIGINT/SIGTERM to stop)\n",
                device.localizedName.UTF8String, outputPath.UTF8String);

        // Signal handlers only set an async-signal-safe flag; the actual
        // Cocoa/AVFoundation calls to stop happen here on the main thread.
        while (!gStopRequested) {
            usleep(50 * 1000);
        }

        [fileOutput stopRecording];

        // Give the delegate callback (delivered asynchronously) a bounded
        // window to confirm the file was flushed before we exit and the Go
        // side tries to read it.
        long timedOut = dispatch_semaphore_wait(
            delegate.doneSemaphore, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
        [session stopRunning];

        if (timedOut != 0) {
            fprintf(stderr, "timed out waiting for recording to finish flushing\n");
            return 3;
        }
        if (delegate.finishError != nil) {
            fprintf(stderr, "recording finished with an error: %s\n",
                    delegate.finishError.localizedDescription.UTF8String);
            return 3;
        }
        return 0;
    }
}
