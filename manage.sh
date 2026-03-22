#!/bin/bash

APP_NAME="zeroclawdash"
APP_PATH="/usr/local/bin/${APP_NAME}"
PID_FILE="/var/run/${APP_NAME}.pid"
LOG_FILE="/var/log/${APP_NAME}.log"

case "$1" in
    start)
        if [ -f "$PID_FILE" ]; then
            PID=$(cat "$PID_FILE")
            if ps -p "$PID" > /dev/null 2>&1; then
                echo "$APP_NAME is already running (PID: $PID)"
                exit 1
            else
                rm -f "$PID_FILE"
            fi
        fi
        
        echo "Starting $APP_NAME..."
        nohup "$APP_PATH" > "$LOG_FILE" 2>&1 &
        echo $! > "$PID_FILE"
        echo "$APP_NAME started successfully (PID: $!)"
        echo "Log file: $LOG_FILE"
        ;;
    
    stop)
        if [ ! -f "$PID_FILE" ]; then
            echo "$APP_NAME is not running"
            exit 1
        fi
        
        PID=$(cat "$PID_FILE")
        if ps -p "$PID" > /dev/null 2>&1; then
            echo "Stopping $APP_NAME (PID: $PID)..."
            kill "$PID"
            sleep 2
            if ps -p "$PID" > /dev/null 2>&1; then
                echo "Force killing $APP_NAME..."
                kill -9 "$PID"
            fi
            rm -f "$PID_FILE"
            echo "$APP_NAME stopped successfully"
        else
            echo "$APP_NAME is not running"
            rm -f "$PID_FILE"
        fi
        ;;
    
    status)
        if [ -f "$PID_FILE" ]; then
            PID=$(cat "$PID_FILE")
            if ps -p "$PID" > /dev/null 2>&1; then
                echo "$APP_NAME is running (PID: $PID)"
                echo "Log file: $LOG_FILE"
                exit 0
            else
                echo "$APP_NAME is not running (stale PID file)"
                rm -f "$PID_FILE"
                exit 1
            fi
        else
            echo "$APP_NAME is not running"
            exit 1
        fi
        ;;
    
    restart)
        echo "Restarting $APP_NAME..."
        $0 stop
        sleep 1
        $0 start
        ;;
    
    logs)
        if [ -f "$LOG_FILE" ]; then
            tail -f "$LOG_FILE"
        else
            echo "Log file not found: $LOG_FILE"
            exit 1
        fi
        ;;
    
    *)
        echo "Usage: $0 {start|stop|status|restart|logs}"
        echo ""
        echo "Commands:"
        echo "  start   - Start the service"
        echo "  stop    - Stop the service"
        echo "  status  - Show service status"
        echo "  restart - Restart the service"
        echo "  logs    - Show real-time logs"
        exit 1
        ;;
esac

exit 0
